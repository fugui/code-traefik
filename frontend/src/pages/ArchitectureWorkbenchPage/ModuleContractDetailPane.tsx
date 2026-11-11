import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import {
  ChevronRight,
  Server,
  Layers,
  Cpu,
  Plus,
  Trash2,
  Globe,
  Link2,
  Unlink,
  CheckCircle2,
  AlertTriangle,
  FileCode,
  Edit3,
} from 'lucide-react';
import { Drawer, Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModulePath } from '../../hooks/useModulePath';
import * as archApi from '../../api/arch';
import type {
  Module,
  ModuleService,
  ModuleDependency,
  AvailableServiceItem,
  ClaimableProtoServiceItem,
  ServiceContract,
  Subsystem,
  FunctionalGroupTreeNode,
  Process,
} from '../../types';

export interface ModuleContractDetailPaneProps {
  modules?: Module[];
  selectedModuleId?: string;
  onSelectModule: (id: string) => void;
  selectedModule: Module | null;
  services: ModuleService[];
  dependencies: ModuleDependency[];
  allProcesses: Process[];
  isAdmin: boolean;
  currentSubsystem?: Subsystem | null;
  currentGroup?: FunctionalGroupTreeNode | null;
  onCreateModule: () => void;
  onEditModule: (mod: Module) => void;
  onDeleteModule: (id: string) => void;
  onViewProcess?: (processId: string) => void;
  onViewArchProcess?: (archProcessId: string | number) => void;
  onCreateService: (data: {
    service_name: string;
    contract_id?: string | number | null;
    impl_key?: string | null;
    diff_ignore_paths?: string[];
    description?: string;
  }) => Promise<void>;
  onUpdateServiceIgnorePaths: (serviceId: string, paths: string[]) => Promise<void>;
  onDeleteService: (serviceId: string) => Promise<void>;
  onCreateDependency: (data: {
    is_external: boolean;
    external_system?: string;
    external_service?: string;
    service_id?: string;
    contract_id?: string | number;
    description?: string;
  }) => Promise<void>;
  onDeleteDependency: (depId: string) => Promise<void>;
  onBindProcess: (moduleId: string, processId: string | null) => Promise<void>;
}

export const ModuleContractDetailPane: React.FC<ModuleContractDetailPaneProps> = ({
  modules,
  onSelectModule,
  selectedModule,
  services,
  dependencies,
  allProcesses,
  isAdmin,
  currentSubsystem,
  currentGroup,
  onCreateModule,
  onEditModule,
  onDeleteModule,
  onViewProcess,
  onViewArchProcess,
  onCreateService,
  onUpdateServiceIgnorePaths,
  onDeleteService,
  onCreateDependency,
  onDeleteDependency,
  onBindProcess,
}) => {
  // 内联编辑忽略路径状态
  const toast = useToast();
  const modulePath = useModulePath();
  const [editingServiceId, setEditingServiceId] = useState<string | null>(null);
  const [ignorePathInput, setIgnorePathInput] = useState<string>('');

  // 依赖拦截详情 Modal 状态
  const [blockedService, setBlockedService] = useState<ModuleService | null>(null);

  const handleDeleteServiceClick = (svc: ModuleService) => {
    if ((svc.downstream_count || 0) > 0) {
      setBlockedService(svc);
      toast.warning(`服务契约 [${svc.service_name}] 正在被其他模块依赖消费，无法直接删除`);
      return;
    }
    void onDeleteService(svc.id);
  };

  // 1. 声明供给服务 Drawer
  const [isProvideDrawerOpen, setIsProvideDrawerOpen] = useState(false);
  const [provideSubmitting, setProvideSubmitting] = useState(false);
  const [provideMode, setProvideMode] = useState<'CLAIM' | 'MANUAL'>('CLAIM');
  const [claimableServices, setClaimableServices] = useState<ClaimableProtoServiceItem[]>([]);
  const [loadingClaimable, setLoadingClaimable] = useState(false);
  const [provideForm, setProvideForm] = useState({
    service_name: '',
    impl_key: '',
    diff_ignore_paths: '',
    description: '',
  });

  // M20 接口契约库 (CONTRACT_LIB) 相关状态
  const isContractLib = selectedModule?.module_kind === 'CONTRACT_LIB';
  const [contracts, setContracts] = useState<ServiceContract[]>([]);
  const [loadingContracts, setLoadingContracts] = useState(false);
  const [selectedContractId, setSelectedContractId] = useState<string | number | null>(null);
  const [isContractDrawerOpen, setIsContractDrawerOpen] = useState(false);
  const [contractSubmitting, setContractSubmitting] = useState(false);
  const [contractForm, setContractForm] = useState({
    service_name: '',
    impl_key_label: '',
    description: '',
  });

  // 全系统已登记多实现契约缓存（供普通业务模块在声明服务时自动检测）
  const [allContracts, setAllContracts] = useState<ServiceContract[]>([]);

  const loadContracts = React.useCallback(async (moduleId: string) => {
    setLoadingContracts(true);
    try {
      const res = await archApi.listServiceContracts({ definer_module_id: moduleId });
      const items = res.items ?? [];
      setContracts(items);
      if (items.length > 0 && !selectedContractId) {
        setSelectedContractId(items[0].id);
      }
    } catch {
      setContracts([]);
    } finally {
      setLoadingContracts(false);
    }
  }, [selectedContractId]);

  const loadAllContracts = React.useCallback(async () => {
    try {
      const res = await archApi.listServiceContracts({ page_size: 200 });
      setAllContracts(res.items ?? []);
    } catch {
      setAllContracts([]);
    }
  }, []);

  useEffect(() => {
    if (selectedModule?.id && isContractLib) {
      void loadContracts(selectedModule.id);
    }
  }, [selectedModule?.id, isContractLib, loadContracts]);

  useEffect(() => {
    void loadAllContracts();
  }, [loadAllContracts]);

  // 打开声明供给服务抽屉时自动加载已激活的 Proto 服务
  useEffect(() => {
    if (isProvideDrawerOpen) {
      setLoadingClaimable(true);
      void archApi
        .listClaimableProtoServices()
        .then((res) => {
          setClaimableServices(res.items ?? []);
        })
        .catch(() => {
          setClaimableServices([]);
        })
        .finally(() => {
          setLoadingClaimable(false);
        });
      void loadAllContracts();
    }
  }, [isProvideDrawerOpen, loadAllContracts]);

  const selectedClaimableItem = claimableServices.find(
    (s) => s.service_name === provideForm.service_name
  );
  const matchedContract = allContracts.find(
    (c) => c.service_name === provideForm.service_name
  );

  // 2. 添加服务依赖 Drawer
  const [isDependDrawerOpen, setIsDependDrawerOpen] = useState(false);
  const [dependSubmitting, setDependSubmitting] = useState(false);
  const [dependType, setDependType] = useState<'INTERNAL' | 'EXTERNAL'>('INTERNAL');
  const [dependForm, setDependForm] = useState({
    service_id: '',
    contract_id: '',
    depend_mode: 'DISPATCH' as 'DISPATCH' | 'SPECIFIC',
    external_system: '',
    external_service: '',
    description: '',
  });
  const [availableServices, setAvailableServices] = useState<AvailableServiceItem[]>([]);
  const [loadingAvailable, setLoadingAvailable] = useState(false);

  const selectedContract = contracts.find((c) => String(c.id) === String(selectedContractId)) || contracts[0] || null;
  const selectedAvailableService = availableServices.find((s) => String(s.service_id) === String(dependForm.service_id));

  // 3. 物理进程装配/更换 Drawer
  const [isProcessDrawerOpen, setIsProcessDrawerOpen] = useState(false);
  const [bindingSubmitting, setBindingSubmitting] = useState(false);
  const [selectedProcessIdToBind, setSelectedProcessIdToBind] = useState<string>('');

  // 打开依赖抽屉时自动加载可用服务
  useEffect(() => {
    if (isDependDrawerOpen && selectedModule) {
      setLoadingAvailable(true);
      void archApi
        .listAvailableServices(selectedModule.id)
        .then((res) => {
          setAvailableServices(res.items ?? []);
        })
        .catch(() => {
          setAvailableServices([]);
        })
        .finally(() => {
          setLoadingAvailable(false);
        });
    }
  }, [isDependDrawerOpen, selectedModule]);

  // 打开进程绑定抽屉时同步当前已有绑定
  useEffect(() => {
    if (isProcessDrawerOpen && selectedModule) {
      setSelectedProcessIdToBind(selectedModule.process_id || '');
    }
  }, [isProcessDrawerOpen, selectedModule]);

  // 内联编辑忽略路径处理
  const handleStartEditPaths = (svc: ModuleService) => {
    setEditingServiceId(svc.id);
    setIgnorePathInput((svc.diff_ignore_paths ?? []).join(', '));
  };

  const handleSavePaths = async (svcId: string) => {
    const paths = ignorePathInput
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    await onUpdateServiceIgnorePaths(svcId, paths);
    setEditingServiceId(null);
  };

  // 提交供给服务
  const handleSubmitProvide = async () => {
    if (!provideForm.service_name.trim()) return;
    if (matchedContract && !provideForm.impl_key.trim()) {
      toast.error('实现多实现契约时，分支标识 (impl_key) 为必填项');
      return;
    }
    if (provideMode === 'CLAIM' && selectedClaimableItem?.is_claimed && !matchedContract) return;
    setProvideSubmitting(true);
    try {
      const paths = provideForm.diff_ignore_paths
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      await onCreateService({
        service_name: provideForm.service_name.trim(),
        contract_id: matchedContract ? matchedContract.id : undefined,
        impl_key: matchedContract ? provideForm.impl_key.trim() : undefined,
        diff_ignore_paths: paths,
        description: provideForm.description.trim(),
      });
      setIsProvideDrawerOpen(false);
      setProvideForm({ service_name: '', impl_key: '', diff_ignore_paths: '', description: '' });
      setProvideMode('CLAIM');
    } finally {
      setProvideSubmitting(false);
    }
  };

  // 提交登记多实现服务契约 (CONTRACT_LIB)
  const handleSubmitContract = async () => {
    if (!selectedModule?.id) return;
    if (!contractForm.service_name.trim()) {
      toast.error('请输入或选择服务契约名称');
      return;
    }
    setContractSubmitting(true);
    try {
      await archApi.createServiceContract({
        definer_module_id: selectedModule.id,
        service_name: contractForm.service_name.trim(),
        impl_key_label: contractForm.impl_key_label.trim(),
        description: contractForm.description.trim(),
      });
      toast.success(`多实现契约 [${contractForm.service_name}] 登记成功`);
      setIsContractDrawerOpen(false);
      setContractForm({ service_name: '', impl_key_label: '', description: '' });
      await loadContracts(selectedModule.id);
      await loadAllContracts();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '登记服务契约失败');
    } finally {
      setContractSubmitting(false);
    }
  };

  const handleDeleteContract = async (id: string | number) => {
    try {
      await archApi.deleteServiceContract(id);
      toast.success('服务契约已删除');
      if (selectedModule?.id) {
        await loadContracts(selectedModule.id);
      }
      await loadAllContracts();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除契约失败');
    }
  };

  // 提交依赖服务
  const handleSubmitDepend = async () => {
    setDependSubmitting(true);
    try {
      if (dependType === 'INTERNAL') {
        const target = availableServices.find((s) => String(s.service_id) === String(dependForm.service_id));
        if (target?.contract_id && dependForm.depend_mode === 'DISPATCH') {
          await onCreateDependency({
            is_external: false,
            contract_id: target.contract_id,
            description: dependForm.description.trim(),
          });
        } else {
          await onCreateDependency({
            is_external: false,
            service_id: dependForm.service_id,
            description: dependForm.description.trim(),
          });
        }
      } else {
        await onCreateDependency({
          is_external: true,
          external_system: dependForm.external_system.trim(),
          external_service: dependForm.external_service.trim(),
          description: dependForm.description.trim(),
        });
      }
      setIsDependDrawerOpen(false);
      setDependForm({
        service_id: '',
        contract_id: '',
        depend_mode: 'DISPATCH',
        external_system: '',
        external_service: '',
        description: '',
      });
    } finally {
      setDependSubmitting(false);
    }
  };

  // 提交进程绑定
  const handleSubmitBindProcess = async () => {
    if (!selectedModule) return;
    setBindingSubmitting(true);
    try {
      await onBindProcess(selectedModule.id, selectedProcessIdToBind || null);
      setIsProcessDrawerOpen(false);
    } finally {
      setBindingSubmitting(false);
    }
  };

  // 解除进程装载快捷处理
  const handleUnbindProcessDirect = async () => {
    if (!selectedModule) return;
    await onBindProcess(selectedModule.id, null);
  };

  return (
    <div className="code-module-pane" data-testid="module-contract-detail-pane">
      {/* 顶部面包屑与快捷操作栏 */}
      <div className="code-module-breadcrumb-bar">
        <div className="code-module-breadcrumb">
          <span className="code-module-breadcrumb__item">架构根节点</span>
          {(currentSubsystem || selectedModule?.subsystem) && (
            <>
              <ChevronRight size={13} className="code-module-breadcrumb__sep" />
              <span className="code-module-breadcrumb__item">
                {currentSubsystem?.name || selectedModule?.subsystem?.name}
              </span>
            </>
          )}
          {(currentGroup || selectedModule?.group) && (
            <>
              <ChevronRight size={13} className="code-module-breadcrumb__sep" />
              <span
                className={`code-module-breadcrumb__item ${
                  !selectedModule ? 'code-module-breadcrumb__item--current' : ''
                }`}
              >
                {currentGroup?.name || selectedModule?.group?.name}
              </span>
            </>
          )}
          {selectedModule && (
            <>
              <ChevronRight size={13} className="code-module-breadcrumb__sep" />
              <span className="code-module-breadcrumb__item code-module-breadcrumb__item--current">
                {selectedModule.name}
              </span>
            </>
          )}
        </div>

        {isAdmin && (
          <button
            type="button"
            className="code-btn code-btn--primary code-btn--xs"
            onClick={onCreateModule}
            title="新建功能模块"
          >
            <Plus size={13} /> 新建模块
          </button>
        )}
      </div>

      {/* 模块主体内容 */}
      {selectedModule ? (
        <div className="code-module-pane__body" style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
          {/* 上半部分：模块基本信息与物理进程装配概览（表单风格卡片） */}
          <div className="code-module-form-card">
            {/* 卡片头部 */}
            <div className="code-module-form-header">
              <div className="code-module-form-title-group">
                <div className="code-module-form-title-row">
                  <h3 className="code-module-form-title">{selectedModule.name}</h3>
                  <span className="code-badge code-badge--info code-font-mono">{selectedModule.code}</span>
                  {isContractLib ? (
                    <span className="code-badge code-badge--primary" data-testid="module-kind-badge" title="接口契约库：仅定义 .pb 契约，供跨进程业务模块多实现">
                      «interface» 接口契约库
                    </span>
                  ) : selectedModule.process ? (
                    <span className="code-badge code-badge--success" title="该功能模块已装配进具体物理进程">
                      <CheckCircle2 size={12} style={{ marginRight: '0.25rem' }} />
                      已装载物理进程
                    </span>
                  ) : (
                    <span className="code-badge code-badge--secondary" title="未关联物理进程，处于逻辑定义阶段">
                      <AlertTriangle size={12} style={{ marginRight: '0.25rem' }} />
                      未装载进程 (逻辑定义)
                    </span>
                  )}
                </div>
                <p className="code-module-form-desc">{selectedModule.description || '暂无模块描述信息'}</p>
              </div>

              {isAdmin && (
                <div className="code-module-form-actions">
                  <button
                    type="button"
                    className="code-btn code-btn--secondary code-btn--xs"
                    onClick={() => onEditModule(selectedModule)}
                  >
                    编辑模块
                  </button>
                  <button
                    type="button"
                    className="code-btn code-btn--danger code-btn--xs"
                    onClick={() => onDeleteModule(selectedModule.id)}
                  >
                    <Trash2 size={12} /> 删除模块
                  </button>
                </div>
              )}
            </div>

            {/* 表单排版双列区域 */}
            <div className="code-module-form-sections">
              {/* 区块 1：逻辑架构元数据表单 */}
              <div className="code-module-form-section">
                <div className="code-module-form-section-header">
                  <div className="code-module-form-section-title">
                    <Layers size={15} style={{ color: 'var(--color-primary)' }} />
                    逻辑架构元数据
                  </div>
                  <span className="code-text-xs code-color-muted">逻辑定义</span>
                </div>

                <div className="code-module-form-field-list">
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">模块名称:</span>
                    <span className="code-module-form-field__val" style={{ fontWeight: 600 }}>
                      {selectedModule.name}
                    </span>
                  </div>
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">模块编码:</span>
                    <span className="code-module-form-field__val code-font-mono code-color-primary">
                      {selectedModule.code}
                    </span>
                  </div>
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">所属子系统:</span>
                    <span className="code-module-form-field__val">
                      {currentSubsystem?.name || selectedModule.subsystem?.name || '-'}
                    </span>
                  </div>
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">功能组路径:</span>
                    <span className="code-module-form-field__val">
                      {currentGroup?.path || currentGroup?.name || selectedModule.group?.name || '-'}
                    </span>
                  </div>
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">模块负责人:</span>
                    <span className="code-module-form-field__val">
                      {selectedModule.owner_name || '平台管理员'}
                    </span>
                  </div>
                </div>
              </div>

              {/* 区块 2：物理进程装载全貌表单 (普通模块) 或 接口契约库特性 (CONTRACT_LIB) */}
              {isContractLib ? (
                <div className="code-module-form-section" data-testid="contract-lib-info-section">
                  <div className="code-module-form-section-header">
                    <div className="code-module-form-section-title">
                      <FileCode size={15} style={{ color: 'var(--color-primary)' }} />
                      接口契约库特性 (CONTRACT_LIB)
                    </div>
                  </div>
                  <div className="code-module-form-field-list">
                    <div className="code-module-form-field">
                      <span className="code-module-form-field__label">契约模式:</span>
                      <span className="code-module-form-field__val">
                        <span className="code-badge code-badge--info">«interface» 共享 IDL 契约库</span>
                      </span>
                    </div>
                    <div className="code-module-form-field">
                      <span className="code-module-form-field__label">装载特性:</span>
                      <span className="code-module-form-field__val code-color-muted">
                        不绑定特定宿主进程。契约由具体业务模块在各自互斥进程中实现落地。
                      </span>
                    </div>
                  </div>
                </div>
              ) : (
                <div className="code-module-form-section">
                  <div className="code-module-form-section-header">
                    <div className="code-module-form-section-title">
                      <Server size={15} style={{ color: 'var(--color-primary)' }} />
                      物理进程装配全貌
                    </div>
                  {isAdmin && (
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                      {selectedModule.process ? (
                        <>
                          <button
                            type="button"
                            className="code-btn code-btn--secondary code-btn--xs"
                            onClick={() => setIsProcessDrawerOpen(true)}
                            title="更换承载此功能模块的物理进程"
                          >
                            更换进程
                          </button>
                          <button
                            type="button"
                            className="code-btn code-btn--danger code-btn--xs"
                            onClick={handleUnbindProcessDirect}
                            title="解除物理进程装配关系"
                          >
                            <Unlink size={11} /> 解除
                          </button>
                        </>
                      ) : (
                        <button
                          type="button"
                          className="code-btn code-btn--primary code-btn--xs"
                          onClick={() => setIsProcessDrawerOpen(true)}
                          title="将此功能模块装配到物理基线进程"
                        >
                          <Link2 size={11} /> 立即装载
                        </button>
                      )}
                    </div>
                  )}
                </div>

                <div className="code-module-form-field-list">
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">标准架构进程:</span>
                    <span className="code-module-form-field__val">
                      {selectedModule.arch_process ? (
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                          <span className="code-font-mono" style={{ fontWeight: 600, color: 'var(--color-primary)' }}>
                            {selectedModule.arch_process.name} ({selectedModule.arch_process.code})
                          </span>
                          <span className="code-badge code-badge--secondary" style={{ fontSize: '0.7rem' }}>
                            {selectedModule.arch_process.default_listen_port === 0
                              ? '纯客户端'
                              : `:${selectedModule.arch_process.default_listen_port}`}
                          </span>
                          <button
                            type="button"
                            className="code-btn code-btn--secondary code-btn--xs"
                            onClick={() =>
                              onViewArchProcess?.(
                                selectedModule.arch_process?.id || selectedModule.arch_process_id!
                              )
                            }
                            data-testid="goto-arch-process-btn"
                            title="切换至物理进程树并定位此架构进程"
                          >
                            查看物理进程树
                          </button>
                        </div>
                      ) : (
                        <span className="code-color-muted">未关联标准架构进程</span>
                      )}
                    </span>
                  </div>
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">承载进程:</span>
                    <span className="code-module-form-field__val">
                      {selectedModule.process ? (
                        <Link
                          to={modulePath(
                            `/agents?scope=${selectedModule.process.agent?.scope || 'PRIVATE'}&agent_id=${selectedModule.process.agent_id || selectedModule.process.agent?.id || ''}&process_id=${selectedModule.process.id}`
                          )}
                          className="code-process-link code-badge-link"
                          data-testid="link-process-bloodline"
                          title="前往 Agent 与进程中心查阅并管理该业务进程"
                        >
                          <Server size={14} />
                          {selectedModule.process.name}
                        </Link>
                      ) : (
                        <span className="code-color-muted">未装载进物理进程</span>
                      )}
                    </span>
                  </div>
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">监听端口:</span>
                    <span className="code-module-form-field__val code-font-mono">
                      {selectedModule.process ? (
                        selectedModule.process.listen_port > 0 ? (
                          <span className="code-badge code-badge--info">Port: {selectedModule.process.listen_port}</span>
                        ) : (
                          <span className="code-badge code-badge--muted">纯客户端 (无监听)</span>
                        )
                      ) : (
                        '-'
                      )}
                    </span>
                  </div>
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">宿主 Agent:</span>
                    <span className="code-module-form-field__val">
                      {selectedModule.process?.agent ? (
                        <>
                          <span style={{ fontWeight: 500 }}>{selectedModule.process.agent.name}</span>
                          <span className="code-text-xs code-color-muted">
                            ({selectedModule.process.agent.endpoint})
                          </span>
                          <span className="code-badge code-badge--secondary code-text-xs">
                            {selectedModule.process.agent.scope === 'SHARED' ? '公共基线' : '专属沙箱'}
                          </span>
                        </>
                      ) : selectedModule.process ? (
                        `Agent #${selectedModule.process.agent_id}`
                      ) : (
                        '-'
                      )}
                    </span>
                  </div>
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">启动识别规则:</span>
                    <span className="code-module-form-field__val code-font-mono code-text-xs">
                      {selectedModule.process?.cmd_pattern || (selectedModule.process ? `./bin/${selectedModule.process.name}` : '-')}
                    </span>
                  </div>
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">部署 Scope:</span>
                    <span className="code-module-form-field__val">
                      <span className="code-badge code-badge--secondary code-text-xs">
                        ALL_ENV (基线全环境兼容)
                      </span>
                    </span>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>

          {/* 下半部分：接口契约库模式 (CONTRACT_LIB) 显示契约定义与实现矩阵，普通模式显示双表格 */}
          {isContractLib ? (
            <div className="code-service-section" data-testid="service-contracts-section">
              <div className="code-service-section-header">
                <div className="code-service-section-title-wrap">
                  <div className="code-service-section-title-row">
                    <h4 className="code-service-section-title">
                      多实现契约定义与实现矩阵 (CONTRACTS)
                    </h4>
                    <span className="code-badge code-badge--info" data-testid="contracts-count-badge">
                      {contracts.length} 个契约
                    </span>
                  </div>
                  <p className="code-service-section-desc">
                    当前模块作为接口契约库 (CONTRACT_LIB) 定义多实现 Service 契约。具体业务模块基于 impl_key 实现此契约，消费方通过 DISPATCH 动态分发调用。
                  </p>
                </div>

                {isAdmin && (
                  <button
                    type="button"
                    className="code-btn code-btn--primary code-btn--xs"
                    data-testid="add-contract-btn"
                    onClick={() => {
                      setContractForm({ service_name: '', impl_key_label: '', description: '' });
                      setIsContractDrawerOpen(true);
                    }}
                  >
                    <Plus size={13} /> 登记多实现服务契约
                  </button>
                )}
              </div>

              {/* 服务契约列表 */}
              <div className="code-table-wrapper" style={{ marginBottom: '1.5rem' }}>
                <table className="code-table" data-testid="service-contracts-table">
                  <thead>
                    <tr>
                      <th style={{ width: '28%' }}>服务名称 (service_name)</th>
                      <th style={{ width: '22%' }}>分支标识说明 (impl_key_label)</th>
                      <th style={{ width: '15%' }}>实现分支数</th>
                      <th style={{ width: '15%' }}>DISPATCH 消费方</th>
                      <th style={{ width: '12%' }}>说明</th>
                      {isAdmin && <th style={{ width: '8%' }}>操作</th>}
                    </tr>
                  </thead>
                  <tbody>
                    {loadingContracts ? (
                      <tr>
                        <td colSpan={isAdmin ? 6 : 5} className="code-table-empty">
                          正在加载服务契约...
                        </td>
                      </tr>
                    ) : (
                      <>
                        {contracts.map((ct) => {
                      const isSelected = String(ct.id) === String(selectedContract?.id);
                      const implCount = ct.implementations?.length ?? 0;
                      const consumerCount = ct.consumer_dependencies?.length ?? 0;
                      return (
                        <tr
                          key={ct.id}
                          onClick={() => setSelectedContractId(ct.id)}
                          style={{
                            cursor: 'pointer',
                            backgroundColor: isSelected ? 'var(--color-bg-active, rgba(59, 130, 246, 0.08))' : undefined,
                          }}
                        >
                          <td>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                              <FileCode size={14} style={{ color: 'var(--color-primary)' }} />
                              <span className="code-font-mono" style={{ fontWeight: 600, color: 'var(--color-primary)' }}>
                                {ct.service_name}
                              </span>
                            </div>
                          </td>
                          <td>
                            <span className="code-tag code-tag--muted">
                              {ct.impl_key_label || 'impl_key'}
                            </span>
                          </td>
                          <td>
                            <span className={`code-badge ${implCount > 0 ? 'code-badge--success' : 'code-badge--warning'}`}>
                              {implCount} 个实现分支
                            </span>
                          </td>
                          <td>
                            <span className={`code-badge ${consumerCount > 0 ? 'code-badge--info' : 'code-badge--muted'}`}>
                              {consumerCount} 个消费方
                            </span>
                          </td>
                          <td>{ct.description || '-'}</td>
                          {isAdmin && (
                            <td>
                              <button
                                type="button"
                                className="code-btn code-btn--danger code-btn--xs"
                                data-testid={`btn-delete-contract-${ct.id}`}
                                onClick={(e) => {
                                  e.stopPropagation();
                                  void handleDeleteContract(ct.id);
                                }}
                              >
                                删除
                              </button>
                            </td>
                          )}
                        </tr>
                      );
                    })}
                    {contracts.length === 0 && (
                      <tr>
                        <td colSpan={isAdmin ? 6 : 5} className="code-table-empty">
                          暂无登记的多实现服务契约，点击右上角【+ 登记多实现服务契约】添加
                        </td>
                      </tr>
                    )}
                      </>
                    )}
                  </tbody>
                </table>
              </div>

              {/* 选中契约后的实现矩阵卡片 */}
              {selectedContract && (
                <div
                  className="code-module-form-section"
                  data-testid="selected-contract-details"
                  style={{ marginTop: '1rem', border: '1px solid var(--color-border-primary)', borderRadius: 'var(--radius-md)' }}
                >
                  <div className="code-module-form-section-header" style={{ padding: '0.75rem 1rem' }}>
                    <div className="code-module-form-section-title" style={{ fontSize: '0.9rem', fontWeight: 600 }}>
                      契约 [{selectedContract.service_name}] 实现与消费全景
                    </div>
                  </div>

                  <div style={{ padding: '0 1rem 1rem 1rem' }}>
                    <h5 style={{ fontSize: '0.825rem', fontWeight: 600, marginBottom: '0.5rem', color: 'var(--color-text-secondary)' }}>
                      实现分支列表 (IMPLEMENTATIONS)
                    </h5>
                    <div className="code-table-wrapper">
                      <table className="code-table" data-testid="impl-matrix-table">
                        <thead>
                          <tr>
                            <th style={{ width: '20%' }}>实现分支标识 (impl_key)</th>
                            <th style={{ width: '25%' }}>实现模块 (Module)</th>
                            <th style={{ width: '25%' }}>所属标准架构进程 (ArchProcess)</th>
                            <th style={{ width: '15%' }}>负责人</th>
                            <th style={{ width: '15%' }}>最近验证状态</th>
                          </tr>
                        </thead>
                        <tbody>
                          {(selectedContract.implementations ?? []).map((impl) => {
                            const implMod = impl.module;
                            const archProc = implMod?.arch_process || implMod?.process;
                            return (
                              <tr key={impl.id}>
                                <td>
                                  <span className="code-tag code-tag--primary code-font-mono">
                                    {impl.impl_key || '-'}
                                  </span>
                                </td>
                                <td>
                                  {implMod ? (
                                    <button
                                      type="button"
                                      className="code-module-link-chip"
                                      onClick={() => onSelectModule(implMod.id)}
                                      title={`点击聚焦模块 ${implMod.name}`}
                                    >
                                      <Cpu size={12} />
                                      <span>{implMod.name}</span>
                                      <span className="code-font-mono code-text-xs">({implMod.code})</span>
                                    </button>
                                  ) : (
                                    '-'
                                  )}
                                </td>
                                <td>
                                  {archProc ? (
                                    <span className="code-process-link">
                                      <Server size={12} />
                                      {archProc.name}
                                    </span>
                                  ) : (
                                    <span className="code-color-muted code-text-xs">未绑定架构进程</span>
                                  )}
                                </td>
                                <td>{implMod?.owner_name || '平台管理员'}</td>
                                <td>
                                  <span className="code-badge code-badge--success">ACTIVE</span>
                                </td>
                              </tr>
                            );
                          })}
                          {(!selectedContract.implementations || selectedContract.implementations.length === 0) && (
                            <tr>
                              <td colSpan={5} className="code-table-empty">
                                暂无业务模块实现该契约分支。业务模块可在其对外供给服务中声明此契约实现。
                              </td>
                            </tr>
                          )}
                        </tbody>
                      </table>
                    </div>

                    <h5 style={{ fontSize: '0.825rem', fontWeight: 600, marginTop: '1.25rem', marginBottom: '0.5rem', color: 'var(--color-text-secondary)' }}>
                      消费此契约的 FM 列表 (DISPATCH CONSUMERS)
                    </h5>
                    <div className="code-table-wrapper">
                      <table className="code-table" data-testid="dispatch-consumers-table">
                        <thead>
                          <tr>
                            <th style={{ width: '35%' }}>消费模块 (Consumer FM)</th>
                            <th style={{ width: '25%' }}>依赖模式</th>
                            <th style={{ width: '40%' }}>说明</th>
                          </tr>
                        </thead>
                        <tbody>
                          {(selectedContract.consumer_dependencies ?? []).map((dep) => {
                            const cMod = dep.module;
                            return (
                              <tr key={dep.id}>
                                <td>
                                  {cMod ? (
                                    <button
                                      type="button"
                                      className="code-module-link-chip"
                                      onClick={() => onSelectModule(cMod.id)}
                                      title={`点击聚焦消费模块 ${cMod.name}`}
                                    >
                                      <Cpu size={12} />
                                      <span>{cMod.name}</span>
                                      <span className="code-font-mono code-text-xs">({cMod.code})</span>
                                    </button>
                                  ) : (
                                    '-'
                                  )}
                                </td>
                                <td>
                                  <span className="code-badge code-badge--info">DISPATCH 动态分发</span>
                                </td>
                                <td>{dep.description || '-'}</td>
                              </tr>
                            );
                          })}
                          {(!selectedContract.consumer_dependencies || selectedContract.consumer_dependencies.length === 0) && (
                            <tr>
                              <td colSpan={3} className="code-table-empty">
                                暂无业务模块声明对此契约的 DISPATCH 依赖
                              </td>
                            </tr>
                          )}
                        </tbody>
                      </table>
                    </div>
                  </div>
                </div>
              )}
            </div>
          ) : (
            <>
              {/* 上表格：对外供给服务列表 (PROVIDE Services) */}
          <div className="code-service-section">
            <div className="code-service-section-header">
              <div className="code-service-section-title-wrap">
                <div className="code-service-section-title-row">
                  <h4 className="code-service-section-title">
                    对外供给服务列表 (PROVIDE)
                  </h4>
                  <span className="code-badge code-badge--success">{services.length} 个服务</span>
                </div>
                <p className="code-service-section-desc">
                  当前模块作为服务端向外界暴露提供的 Service 服务契约，全平台 service_name 保持全局唯一
                </p>
              </div>

              {isAdmin && (
                <button
                  type="button"
                  className="code-btn code-btn--primary code-btn--xs"
                  onClick={() => {
                    setProvideForm({ service_name: '', impl_key: '', diff_ignore_paths: '', description: '' });
                    setIsProvideDrawerOpen(true);
                  }}
                >
                  <Plus size={13} /> 声明供给服务
                </button>
              )}
            </div>

            <div className="code-table-wrapper">
              <table className="code-table">
                <thead>
                  <tr>
                    <th style={{ width: '28%' }}>服务名称 (service_name)</th>
                    <th style={{ width: '32%' }}>默认 Diff 忽略字段路径 (diff_ignore_paths)</th>
                    <th style={{ width: '15%' }}>服务消费方 (Consumers)</th>
                    <th style={{ width: '15%' }}>说明</th>
                    {isAdmin && <th style={{ width: '10%' }}>操作</th>}
                  </tr>
                </thead>
                <tbody>
                  {services.map((svc) => (
                    <tr key={svc.id}>
                      <td>
                        <Link
                          to={modulePath(`/schemas?active_only=true&search=${encodeURIComponent(svc.service_name)}`)}
                          className="code-font-mono code-color-primary code-service-contract-link"
                          style={{ fontWeight: 600 }}
                          data-testid={`link-service-${svc.id}`}
                          title="前往 Proto 契约工作台查阅并配置字段树"
                        >
                          {svc.service_name}
                        </Link>
                      </td>
                      <td>
                        {editingServiceId === svc.id ? (
                          <div className="code-ignore-path-edit-inline">
                            <input
                              type="text"
                              className="code-input code-input--sm"
                              value={ignorePathInput}
                              onChange={(e) => setIgnorePathInput(e.target.value)}
                              placeholder="逗号分隔，如 header.timestamp, items[*].id"
                            />
                            <button
                              type="button"
                              className="code-btn code-btn--primary code-btn--xs"
                              onClick={() => void handleSavePaths(svc.id)}
                            >
                              保存
                            </button>
                            <button
                              type="button"
                              className="code-btn code-btn--secondary code-btn--xs"
                              onClick={() => setEditingServiceId(null)}
                            >
                              取消
                            </button>
                          </div>
                        ) : (
                          <div className="code-ignore-path-chips">
                            {(svc.diff_ignore_paths ?? []).map((path, idx) => (
                              <span key={idx} className="code-tag code-tag--muted code-font-mono">
                                {path}
                              </span>
                            ))}
                            {(!svc.diff_ignore_paths || svc.diff_ignore_paths.length === 0) && (
                              <span className="code-color-muted code-text-xs">无</span>
                            )}
                            {isAdmin && (
                              <button
                                type="button"
                                className="code-btn-text code-text-xs"
                                onClick={() => handleStartEditPaths(svc)}
                                title="编辑 Diff 忽略路径"
                              >
                                ✎
                              </button>
                            )}
                          </div>
                        )}
                      </td>
                      <td>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: '4px', alignItems: 'flex-start' }}>
                          <span
                            className={`code-badge ${(svc.downstream_count || 0) > 0 ? 'code-badge--warning' : 'code-badge--info'}`}
                            data-testid={`badge-downstream-${svc.id}`}
                          >
                            {svc.downstream_count || 0} 个消费方
                          </span>
                          {svc.downstream_consumers && svc.downstream_consumers.length > 0 ? (
                            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', maxWidth: '260px' }}>
                              {svc.downstream_consumers.map((c) => (
                                <button
                                  key={c.module_id}
                                  type="button"
                                  className="code-module-link-chip"
                                  style={{ fontSize: '11px', padding: '2px 6px' }}
                                  onClick={() => onSelectModule(String(c.module_id))}
                                  title={`点击在架构树中聚焦此消费模块：${c.module_name}${c.subsystem_name ? ` (${c.subsystem_name})` : ''}`}
                                >
                                  <Cpu size={11} />
                                  <span>{c.module_name}</span>
                                  {c.subsystem_name && (
                                    <span className="code-color-muted code-text-xs">[{c.subsystem_name}]</span>
                                  )}
                                </button>
                              ))}
                            </div>
                          ) : svc.downstream_modules && svc.downstream_modules.length > 0 ? (
                            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', maxWidth: '260px' }}>
                              {svc.downstream_modules.map((modName, idx) => (
                                <span
                                  key={idx}
                                  className="code-tag code-tag--muted code-font-mono"
                                  style={{ fontSize: '11px' }}
                                  title={`服务消费方: ${modName}`}
                                >
                                  {modName}
                                </span>
                              ))}
                            </div>
                          ) : null}
                        </div>
                      </td>
                      <td>{svc.description || '-'}</td>
                      {isAdmin && (
                        <td>
                          <button
                            type="button"
                            className={`code-btn code-btn--xs ${(svc.downstream_count || 0) > 0 ? 'code-btn--secondary' : 'code-btn--danger'}`}
                            title={
                              (svc.downstream_count || 0) > 0
                                ? `正被 ${svc.downstream_count} 个模块依赖消费，点击查看消费详情与解除指引`
                                : `删除对外服务契约 [${svc.service_name}]`
                            }
                            data-testid={`btn-delete-service-${svc.id}`}
                            onClick={() => handleDeleteServiceClick(svc)}
                          >
                            {(svc.downstream_count || 0) > 0 ? (
                              <span style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
                                <AlertTriangle size={12} style={{ color: 'var(--color-warning)' }} />
                                删除
                              </span>
                            ) : (
                              '删除'
                            )}
                          </button>
                        </td>
                      )}
                    </tr>
                  ))}
                  {services.length === 0 && (
                    <tr>
                      <td colSpan={isAdmin ? 5 : 4} className="code-table-empty">
                        暂无对外供给服务契约，点击右上角【+ 声明供给服务】添加
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>

          {/* 下表格：所需依赖服务列表 (DEPEND Services) */}
          <div className="code-service-section">
            <div className="code-service-section-header">
              <div className="code-service-section-title-wrap">
                <div className="code-service-section-title-row">
                  <h4 className="code-service-section-title">
                    所需依赖服务列表 (DEPEND)
                  </h4>
                  <span className="code-badge code-badge--info">{dependencies.length} 个依赖</span>
                </div>
                <p className="code-service-section-desc">
                  当前模块运行所依赖的 Service 服务契约（支持平台内已有模块供给服务或外部第三方系统）
                </p>
              </div>

              {isAdmin && (
                <button
                  type="button"
                  className="code-btn code-btn--secondary code-btn--xs"
                  onClick={() => {
                    setDependType('INTERNAL');
                    setDependForm({
                      service_id: '',
                      contract_id: '',
                      depend_mode: 'DISPATCH',
                      external_system: '',
                      external_service: '',
                      description: '',
                    });
                    setIsDependDrawerOpen(true);
                  }}
                >
                  <Plus size={13} /> 添加服务依赖
                </button>
              )}
            </div>

            <div className="code-table-wrapper">
              <table className="code-table">
                <thead>
                  <tr>
                    <th style={{ width: '26%' }}>依赖服务名称</th>
                    <th style={{ width: '22%' }}>供给方来源 (Provider FM / 外部系统)</th>
                    <th style={{ width: '18%' }}>所属子系统</th>
                    <th style={{ width: '16%' }}>供给方承载进程</th>
                    <th style={{ width: '10%' }}>说明</th>
                    {isAdmin && <th style={{ width: '8%' }}>操作</th>}
                  </tr>
                </thead>
                <tbody>
                  {dependencies.map((dep) => {
                    const svcName = dep.is_external
                      ? dep.external_service
                      : dep.service?.service_name || dep.service_id;
                    const providerMod = dep.provider_module;
                    const providerSubsystemName = dep.is_external
                      ? `外部第三方 (${dep.external_system})`
                      : providerMod?.subsystem?.name || '-';
                    const archProc = providerMod?.arch_process;
                    const proc = archProc || providerMod?.process;
                    const procPort = archProc
                      ? archProc.default_listen_port
                      : proc && 'listen_port' in proc
                      ? proc.listen_port
                      : 0;

                    return (
                      <tr key={dep.id}>
                        <td>
                          <span className="code-font-mono code-color-primary" style={{ fontWeight: 600 }}>
                            {svcName}
                          </span>
                        </td>
                        <td>
                          {dep.is_external ? (
                            <span className="code-badge code-badge--warning">
                              <Globe size={11} style={{ marginRight: '0.25rem' }} />
                              外部系统: {dep.external_system}
                            </span>
                          ) : providerMod ? (
                            <button
                              type="button"
                              className="code-module-link-chip"
                              onClick={() => onSelectModule(providerMod.id)}
                              title="点击在架构树中聚焦此供给模块"
                            >
                              <Cpu size={12} />
                              <span>{providerMod.name}</span>
                              <span className="code-font-mono code-text-xs">({providerMod.code})</span>
                            </button>
                          ) : (
                            <span className="code-color-muted">未知模块</span>
                          )}
                        </td>
                        <td>{providerSubsystemName}</td>
                        <td>
                          {dep.is_external ? (
                            <span className="code-color-muted code-text-xs">外部环境自理</span>
                          ) : proc ? (
                            <span
                              className="code-process-link"
                              onClick={() => {
                                if (archProc && onViewArchProcess) {
                                  onViewArchProcess(archProc.id);
                                } else if (onViewProcess && 'id' in proc && proc.id) {
                                  onViewProcess(String(proc.id));
                                }
                              }}
                              title={
                                archProc && onViewArchProcess
                                  ? '在物理进程树中定位此架构进程'
                                  : onViewProcess
                                  ? '在物理架构树中定位此承载进程'
                                  : undefined
                              }
                            >
                              <Server size={13} />
                              {proc.name} {procPort > 0 ? `(Port: ${procPort})` : '(纯客户端)'}
                            </span>
                          ) : (
                            <span className="code-color-muted code-text-xs">未装配进程</span>
                          )}
                        </td>
                        <td>{dep.description || '-'}</td>
                        {isAdmin && (
                          <td>
                            <button
                              type="button"
                              className="code-btn code-btn--danger code-btn--xs"
                              onClick={() => {
                                void onDeleteDependency(dep.id);
                              }}
                            >
                              解除
                            </button>
                          </td>
                        )}
                      </tr>
                    );
                  })}
                  {dependencies.length === 0 && (
                    <tr>
                      <td colSpan={isAdmin ? 6 : 5} className="code-table-empty">
                        当前模块暂未声明任何服务依赖，点击右上角【+ 添加服务依赖】声明链路
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
          </>
          )}
        </div>
      ) : (
        <div
          className="code-empty-pane code-p-lg"
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            minHeight: '320px',
            gap: '0.75rem',
            textAlign: 'center',
          }}
        >
          {currentGroup ? (
            <>
              <Layers size={36} style={{ color: 'var(--color-primary)', opacity: 0.8 }} />
              <h3 style={{ fontSize: '1.125rem', fontWeight: 600, color: 'var(--color-text-primary)', margin: 0 }}>
                {currentGroup.name}
              </h3>
              <p
                style={{
                  color: 'var(--color-text-secondary)',
                  fontSize: '0.875rem',
                  maxWidth: '420px',
                  margin: 0,
                  lineHeight: 1.5,
                }}
              >
                {modules && modules.length > 0
                  ? '请在左侧架构元素树中选择具体功能模块以查看或编辑其服务契约与进程绑定。'
                  : '当前功能组下暂无功能模块，您可以点击下方按钮为此功能组创建第一个功能模块。'}
              </p>
              {isAdmin && (!modules || modules.length === 0) && (
                <button
                  type="button"
                  className="code-btn code-btn--primary code-btn--sm"
                  style={{ marginTop: '0.5rem' }}
                  onClick={onCreateModule}
                >
                  <Plus size={14} /> 新建功能模块
                </button>
              )}
            </>
          ) : (
            <>
              <Cpu size={36} style={{ color: 'var(--color-text-muted)', opacity: 0.6 }} />
              <h4 style={{ fontSize: '1rem', fontWeight: 600, color: 'var(--color-text-primary)', margin: 0 }}>
                未选择功能模块
              </h4>
              <p style={{ color: 'var(--color-text-secondary)', fontSize: '0.875rem', margin: 0 }}>
                请在左侧架构元素树中选择子系统、功能组或功能模块
              </p>
            </>
          )}
        </div>
      )}

      {/* ─────────────────────────────────────────────────────────────
          1. 声明供给服务 Drawer
          ───────────────────────────────────────────────────────────── */}
      <Drawer
        open={isProvideDrawerOpen}
        onClose={() => setIsProvideDrawerOpen(false)}
        title={`声明对外供给服务 [${selectedModule?.name || ''}]`}
        subtitle="当前模块作为服务端对外暴露 Service 契约（遵循 A-05 全局唯一供给原则）"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setIsProvideDrawerOpen(false)}
              disabled={provideSubmitting}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSubmitProvide()}
              disabled={
                provideSubmitting ||
                !provideForm.service_name.trim() ||
                (Boolean(matchedContract) && !provideForm.impl_key.trim()) ||
                (provideMode === 'CLAIM' && selectedClaimableItem?.is_claimed && !matchedContract)
              }
            >
              {provideSubmitting ? '保存中...' : '确认声明'}
            </button>
          </div>
        }
      >
        <div className="code-form">
          {/* 声明模式切换 Tab */}
          <div className="code-form-item">
            <label className="code-form-label">服务声明方式 *:</label>
            <div className="code-type-selector-tabs">
              <button
                type="button"
                className={`code-type-selector-tab ${provideMode === 'CLAIM' ? 'code-type-selector-tab--active' : ''}`}
                onClick={() => setProvideMode('CLAIM')}
              >
                <FileCode size={14} style={{ marginRight: '0.35rem' }} />
                从已激活 Proto Service 认领
              </button>
              <button
                type="button"
                className={`code-type-selector-tab ${provideMode === 'MANUAL' ? 'code-type-selector-tab--active' : ''}`}
                onClick={() => setProvideMode('MANUAL')}
              >
                <Edit3 size={14} style={{ marginRight: '0.35rem' }} />
                手动预先输入声明
              </button>
            </div>
          </div>

          {provideMode === 'CLAIM' ? (
            <div className="code-form-item">
              <label className="code-form-label">选择待认领的 Proto Service *:</label>
              {loadingClaimable ? (
                <div className="code-color-muted code-text-sm">正在加载全系统已激活 Proto 服务...</div>
              ) : (
                <select
                  className="code-select"
                  value={provideForm.service_name}
                  onChange={(e) => setProvideForm({ ...provideForm, service_name: e.target.value })}
                >
                  <option value="">-- 请选择全系统已上传并激活的 Proto Service --</option>
                  {claimableServices.map((item) => (
                    <option
                      key={item.service_name}
                      value={item.service_name}
                      disabled={item.is_claimed}
                    >
                      {item.service_name} ({item.operation_count} 个方法 | Schema: {item.schema_name})
                      {item.is_claimed
                        ? ` [已被 ${item.claimed_by_module?.name || '其他模块'} 占用]`
                        : ' [可认领]'}
                    </option>
                  ))}
                </select>
              )}
              {claimableServices.length === 0 && !loadingClaimable && (
                <span className="code-form-help code-color-warning">
                  当前平台暂无已上传或激活的 .pb IDL 描述集。您可以切换至「手动预先输入声明」，或先前往【Proto 描述集】工作台上传激活。
                </span>
              )}

              {/* 选中 Service 的详情画像预览 */}
              {selectedClaimableItem && (
                <div
                  style={{
                    marginTop: '0.75rem',
                    padding: '0.75rem',
                    borderRadius: 'var(--radius-md, 6px)',
                    background: 'var(--color-bg-muted, rgba(0,0,0,0.02))',
                    border: '1px solid var(--color-border-primary, rgba(0,0,0,0.08))',
                  }}
                >
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      marginBottom: '0.5rem',
                    }}
                  >
                    <span style={{ fontWeight: 600, color: 'var(--color-text-primary)' }}>
                      {selectedClaimableItem.service_name}
                    </span>
                    <span
                      className={`code-badge ${
                        selectedClaimableItem.is_claimed ? 'code-badge--warning' : 'code-badge--success'
                      }`}
                    >
                      {selectedClaimableItem.is_claimed
                        ? `已由 ${selectedClaimableItem.claimed_by_module?.name} [${selectedClaimableItem.claimed_by_module?.code}] 占用`
                        : '可自由认领'}
                    </span>
                  </div>
                  <div style={{ fontSize: '0.8125rem', color: 'var(--color-text-secondary)', marginBottom: '0.5rem' }}>
                    所属 Schema: <strong>{selectedClaimableItem.schema_name}</strong> | 包名:{' '}
                    <code>{selectedClaimableItem.proto_package || '(无)'}</code> | 包含方法数:{' '}
                    <strong>{selectedClaimableItem.operation_count}</strong>
                  </div>
                  {selectedClaimableItem.operations.length > 0 && (
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.35rem', marginTop: '0.25rem' }}>
                      {selectedClaimableItem.operations.map((op) => (
                        <span
                          key={op}
                          style={{
                            fontSize: '0.75rem',
                            padding: '0.15rem 0.4rem',
                            borderRadius: '4px',
                            background: 'var(--color-bg-surface, #fff)',
                            border: '1px solid var(--color-border-primary, #e2e8f0)',
                            color: 'var(--color-text-primary)',
                            fontFamily: 'monospace',
                          }}
                        >
                          {op}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          ) : (
            <div className="code-form-item">
              <label className="code-form-label">服务名称 (service_name) *:</label>
              <input
                type="text"
                className="code-input"
                value={provideForm.service_name}
                onChange={(e) => setProvideForm({ ...provideForm, service_name: e.target.value })}
                placeholder="建议格式: {package}.{serviceName}，如 dophi.order.v1.TradeService"
              />
              <span className="code-form-help">全平台全局唯一的服务契约名称，遵循点分命名规范，其他 FM 或网关将基于此服务名发起调用</span>
            </div>
          )}

          {matchedContract && (
            <div
              className="code-alert code-alert--info"
              data-testid="contract-impl-banner"
              style={{
                padding: '0.75rem',
                borderRadius: 'var(--radius-md)',
                backgroundColor: 'rgba(59, 130, 246, 0.08)',
                border: '1px solid var(--color-primary)',
                color: 'var(--color-primary)',
                marginBottom: '1rem',
                fontSize: '0.85rem',
                display: 'flex',
                alignItems: 'center',
                gap: '0.5rem',
              }}
            >
              <FileCode size={16} />
              <div>
                该服务属于接口契约库 <strong>[{matchedContract.definer_module?.name || '契约库'}]</strong> 定义的多实现契约，请录入本模块的分支标识 (impl_key)。
              </div>
            </div>
          )}

          {matchedContract && (
            <div className="code-form-item" data-testid="impl-key-form-item">
              <label className="code-form-label">
                实现分支标识 (impl_key) *:
                {matchedContract.impl_key_label && (
                  <span className="code-color-muted code-text-xs" style={{ marginLeft: '0.5rem' }}>
                    ({matchedContract.impl_key_label})
                  </span>
                )}
              </label>
              <input
                type="text"
                className="code-input"
                data-testid="provide-impl-key-input"
                value={provideForm.impl_key}
                onChange={(e) => setProvideForm({ ...provideForm, impl_key: e.target.value })}
                placeholder="如 subaccount, international, standard 等"
              />
              <span className="code-form-help">
                多实现契约的分支唯一标识符，同一契约在不同实现模块间必须具有独立且唯一的标准架构进程。
              </span>
            </div>
          )}

          <div className="code-form-item">
            <label className="code-form-label">默认 Diff 忽略字段路径:</label>
            <input
              type="text"
              className="code-input"
              value={provideForm.diff_ignore_paths}
              onChange={(e) => setProvideForm({ ...provideForm, diff_ignore_paths: e.target.value })}
              placeholder="逗号分隔，如 header.timestamp, trace_id, meta[*].token"
            />
            <span className="code-form-help">在流量双跑一致性比对时，该服务将默认忽略比对上述字段路径</span>
          </div>

          <div className="code-form-item">
            <label className="code-form-label">服务说明 / 接口职能:</label>
            <textarea
              className="code-input"
              rows={3}
              value={provideForm.description}
              onChange={(e) => setProvideForm({ ...provideForm, description: e.target.value })}
              placeholder="说明此服务的核心职责与业务范畴..."
            />
          </div>
        </div>
      </Drawer>

      {/* ─────────────────────────────────────────────────────────────
          2. 添加服务依赖 Drawer
          ───────────────────────────────────────────────────────────── */}
      <Drawer
        open={isDependDrawerOpen}
        onClose={() => setIsDependDrawerOpen(false)}
        title={`添加服务依赖 [${selectedModule?.name || ''}]`}
        subtitle="声明当前功能模块在业务链路中需要调用或依赖的 Service 服务契约"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setIsDependDrawerOpen(false)}
              disabled={dependSubmitting}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSubmitDepend()}
              disabled={
                dependSubmitting ||
                (dependType === 'INTERNAL' && !dependForm.service_id) ||
                (dependType === 'EXTERNAL' &&
                  (!dependForm.external_system.trim() || !dependForm.external_service.trim()))
              }
            >
              {dependSubmitting ? '保存中...' : '确认添加依赖'}
            </button>
          </div>
        }
      >
        <div className="code-form">
          {/* 依赖类型选择 Tab */}
          <div className="code-form-item">
            <label className="code-form-label">依赖服务来源类型 *:</label>
            <div className="code-type-selector-tabs">
              <button
                type="button"
                className={`code-type-selector-tab ${dependType === 'INTERNAL' ? 'code-type-selector-tab--active' : ''}`}
                onClick={() => setDependType('INTERNAL')}
              >
                <Cpu size={14} style={{ marginRight: '0.35rem' }} />
                平台内部已有 FM 服务
              </button>
              <button
                type="button"
                className={`code-type-selector-tab ${dependType === 'EXTERNAL' ? 'code-type-selector-tab--active' : ''}`}
                onClick={() => setDependType('EXTERNAL')}
              >
                <Globe size={14} style={{ marginRight: '0.35rem' }} />
                外部第三方系统服务
              </button>
            </div>
          </div>

          {dependType === 'INTERNAL' ? (
            <div className="code-form-item">
              <label className="code-form-label">选择目标服务 (从平台可用服务中选择) *:</label>
              {loadingAvailable ? (
                <div className="code-color-muted code-text-sm">正在加载全平台可用服务...</div>
              ) : (
                <select
                  className="code-select"
                  value={dependForm.service_id}
                  onChange={(e) => setDependForm({ ...dependForm, service_id: e.target.value })}
                >
                  <option value="">-- 请选择被依赖的目标 Service --</option>
                  {availableServices.map((item) => (
                    <option key={item.service_id} value={item.service_id}>
                      {item.service_name}
                      {item.impl_key ? ` [分支: ${item.impl_key}]` : ''}
                      {item.is_multi_impl ? ' [多实现契约]' : ''}
                      {' '}(供给方: {item.module_name} [{item.module_code}] / 子系统: {item.subsystem_name})
                    </option>
                  ))}
                </select>
              )}
              {selectedAvailableService?.contract_id && (
                <div className="code-form-item" data-testid="depend-mode-selector" style={{ marginTop: '0.75rem' }}>
                  <label className="code-form-label">多实现契约依赖模式 *:</label>
                  <div className="code-type-selector-tabs">
                    <button
                      type="button"
                      className={`code-type-selector-tab ${dependForm.depend_mode === 'DISPATCH' ? 'code-type-selector-tab--active' : ''}`}
                      onClick={() => setDependForm({ ...dependForm, depend_mode: 'DISPATCH' })}
                    >
                      DISPATCH 动态分发 (推荐)
                    </button>
                    <button
                      type="button"
                      className={`code-type-selector-tab ${dependForm.depend_mode === 'SPECIFIC' ? 'code-type-selector-tab--active' : ''}`}
                      onClick={() => setDependForm({ ...dependForm, depend_mode: 'SPECIFIC' })}
                    >
                      SPECIFIC 固定具体 FM
                    </button>
                  </div>
                  <span className="code-form-help">
                    DISPATCH 模式下，依赖基于公共 IDL 契约，由主控或 Agent 在运行时依据请求报文 DstTag 或调用端点自动路由至具体下游。
                  </span>
                </div>
              )}
              {availableServices.length === 0 && !loadingAvailable && (
                <span className="code-form-help code-color-warning">
                  当前平台暂无其他模块声明的对外服务，请先在供给方 FM 中声明 Service 契约。
                </span>
              )}
            </div>
          ) : (
            <>
              <div className="code-form-item">
                <label className="code-form-label">外部系统标识 / 名称 *:</label>
                <input
                  type="text"
                  className="code-input"
                  value={dependForm.external_system}
                  onChange={(e) => setDependForm({ ...dependForm, external_system: e.target.value })}
                  placeholder="如 AlipayPaymentGateway, LegacyCRM, SMSPlatform"
                />
                <span className="code-form-help">表示未在架构中心纳管但业务依赖的外部第三方子系统</span>
              </div>
              <div className="code-form-item">
                <label className="code-form-label">外部服务名称 (external_service) *:</label>
                <input
                  type="text"
                  className="code-input"
                  value={dependForm.external_service}
                  onChange={(e) => setDependForm({ ...dependForm, external_service: e.target.value })}
                  placeholder="如 alipay.trade.v1.TradeService"
                />
              </div>
            </>
          )}

          <div className="code-form-item">
            <label className="code-form-label">调用说明 / 业务依赖目的:</label>
            <input
              type="text"
              className="code-input"
              value={dependForm.description}
              onChange={(e) => setDependForm({ ...dependForm, description: e.target.value })}
              placeholder="如 订单下单扣库存前调用此服务进行余额校验..."
            />
          </div>
        </div>
      </Drawer>

      {/* ─────────────────────────────────────────────────────────────
          3. 物理进程装配 / 更换 Drawer
          ───────────────────────────────────────────────────────────── */}
      <Drawer
        open={isProcessDrawerOpen}
        onClose={() => setIsProcessDrawerOpen(false)}
        title={`物理进程装配配置 [${selectedModule?.name || ''}]`}
        subtitle="将当前功能模块装配到公共基线物理进程或专属容器进程"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setIsProcessDrawerOpen(false)}
              disabled={bindingSubmitting}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSubmitBindProcess()}
              disabled={bindingSubmitting}
            >
              {bindingSubmitting ? '保存中...' : '确认装配'}
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">选择承载该功能模块的物理进程:</label>
            <select
              className="code-select"
              value={selectedProcessIdToBind}
              onChange={(e) => setSelectedProcessIdToBind(e.target.value)}
            >
              <option value="">-- 未装配任何物理进程 (解除绑定) --</option>
              {allProcesses.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} {p.listen_port > 0 ? `(Port: ${p.listen_port})` : '(纯客户端)'} [{p.group_name || '默认组'}] - 宿主: {p.agent?.name || `Agent #${p.agent_id}`}
                </option>
              ))}
            </select>
            <span className="code-form-help">
              同一物理进程下装配的多个功能模块之间的服务调用，架构拓扑引擎将自动归因识别为进程内调用 (INTRA_PROCESS)；
              不同物理进程间的服务调用则归因为跨进程 RPC 调用 (CROSS_PROCESS)。
            </span>
          </div>

          {selectedProcessIdToBind && (
            <div className="code-meta-card code-mt-sm" style={{ padding: '0.85rem' }}>
              {(() => {
                const targetProc = allProcesses.find((p) => p.id === selectedProcessIdToBind);
                if (!targetProc) return null;
                return (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem', fontSize: '0.8rem' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontWeight: 600 }}>
                      <Server size={15} style={{ color: 'var(--color-primary)' }} />
                      <span>{targetProc.name}</span>
                      {targetProc.listen_port > 0 ? (
                        <span className="code-badge code-badge--info">Port: {targetProc.listen_port}</span>
                      ) : (
                        <span className="code-badge code-badge--muted">纯客户端</span>
                      )}
                    </div>
                    <div className="code-color-muted">
                      所属 Agent: {targetProc.agent?.name || `Agent #${targetProc.agent_id}`} ({targetProc.agent?.endpoint || '192.168.56.18'})
                    </div>
                    <div className="code-color-muted">
                      进程分组: {targetProc.group_name || '默认组'} | 命令行识别: {targetProc.cmd_pattern || `./bin/${targetProc.name}`}
                    </div>
                  </div>
                );
              })()}
            </div>
          )}
        </div>
      </Drawer>

      {/* ─────────────────────────────────────────────────────────────
          3.5 登记多实现服务契约 Drawer (CONTRACT_LIB)
          ───────────────────────────────────────────────────────────── */}
      <Drawer
        open={isContractDrawerOpen}
        onClose={() => setIsContractDrawerOpen(false)}
        title={`登记多实现服务契约 [${selectedModule?.name || ''}]`}
        subtitle="在接口契约库中定义多实现 Service 契约，供多个具体功能模块以不同 impl_key 分别实现"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setIsContractDrawerOpen(false)}
              disabled={contractSubmitting}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              data-testid="submit-contract-btn"
              onClick={() => void handleSubmitContract()}
              disabled={contractSubmitting || !contractForm.service_name.trim()}
            >
              {contractSubmitting ? '保存中...' : '确认登记'}
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">服务契约名称 (service_name) *:</label>
            <input
              type="text"
              className="code-input"
              data-testid="contract-service-name-input"
              value={contractForm.service_name}
              onChange={(e) => setContractForm({ ...contractForm, service_name: e.target.value })}
              placeholder="建议格式: {package}.{serviceName}，如 dophi.payment.v1.PayService"
            />
            <span className="code-form-help">全平台唯一的服务契约 IDL 标识，对应 .pb 中的点分服务规范</span>
          </div>
          <div className="code-form-item">
            <label className="code-form-label">分支标识说明 (impl_key_label):</label>
            <input
              type="text"
              className="code-input"
              data-testid="contract-impl-key-label-input"
              value={contractForm.impl_key_label}
              onChange={(e) => setContractForm({ ...contractForm, impl_key_label: e.target.value })}
              placeholder="如 渠道标识 (alipay/wechat/unionpay)、账户类型 (personal/enterprise)"
            />
            <span className="code-form-help">说明该契约由各业务模块实现时的分支语义</span>
          </div>
          <div className="code-form-item">
            <label className="code-form-label">契约职责说明:</label>
            <textarea
              className="code-input"
              rows={3}
              value={contractForm.description}
              onChange={(e) => setContractForm({ ...contractForm, description: e.target.value })}
              placeholder="描述该通用服务契约的标准输入输出及业务目标..."
            />
          </div>
        </div>
      </Drawer>

      {/* 4. 依赖拦截指引 Modal */}
      <Modal
        open={!!blockedService}
        onClose={() => setBlockedService(null)}
        title="无法直接删除服务契约"
      >
        <div className="code-blocked-service-modal" style={{ padding: '0.5rem 0' }}>
          <div style={{ display: 'flex', alignItems: 'flex-start', gap: '12px', marginBottom: '1.25rem' }}>
            <div
              style={{
                width: '40px',
                height: '40px',
                borderRadius: '50%',
                backgroundColor: 'rgba(234, 179, 8, 0.12)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                flexShrink: 0,
              }}
            >
              <AlertTriangle size={22} style={{ color: 'var(--color-warning)' }} />
            </div>
            <div>
              <h4 style={{ margin: '0 0 6px 0', fontSize: '15px', color: 'var(--color-text-primary)' }}>
                契约正在被其他模块引用消费
              </h4>
              <p style={{ margin: 0, fontSize: '13px', color: 'var(--color-text-secondary)', lineHeight: 1.6 }}>
                对外服务契约 <code className="code-font-mono" style={{ color: 'var(--color-primary)', fontWeight: 600 }}>{blockedService?.service_name}</code> 当前正被以下 <strong style={{ color: 'var(--color-warning)' }}>{blockedService?.downstream_count}</strong> 个功能模块作为服务依赖消费。为保证系统拓扑完整性与调用链稳定，必须先解除依赖。
              </p>
            </div>
          </div>

          <div style={{ marginBottom: '1.25rem' }}>
            <div style={{ fontSize: '12px', fontWeight: 600, color: 'var(--color-text-secondary)', marginBottom: '8px' }}>
              消费此服务的功能模块清单：
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', maxHeight: '200px', overflowY: 'auto' }}>
              {blockedService?.downstream_consumers && blockedService.downstream_consumers.length > 0 ? (
                blockedService.downstream_consumers.map((c) => (
                  <div
                    key={c.module_id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      padding: '8px 12px',
                      borderRadius: 'var(--radius-md)',
                      backgroundColor: 'var(--color-bg-muted)',
                      border: '1px solid var(--color-border-primary)',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                      <Cpu size={14} style={{ color: 'var(--color-primary)' }} />
                      <span style={{ fontWeight: 600, fontSize: '13px', color: 'var(--color-text-primary)' }}>
                        {c.module_name}
                      </span>
                      <span className="code-tag code-font-mono code-text-xs">
                        {c.module_code}
                      </span>
                      {c.subsystem_name && (
                        <span className="code-text-xs code-color-muted">
                          [{c.subsystem_name}]
                        </span>
                      )}
                    </div>
                    <button
                      type="button"
                      className="code-btn code-btn--primary code-btn--xs"
                      onClick={() => {
                        setBlockedService(null);
                        onSelectModule(String(c.module_id));
                      }}
                      title="点击直接切换并聚焦至该消费模块"
                    >
                      前往解除依赖 →
                    </button>
                  </div>
                ))
              ) : (
                (blockedService?.downstream_modules ?? []).map((modName, idx) => (
                  <div
                    key={idx}
                    style={{
                      padding: '8px 12px',
                      borderRadius: 'var(--radius-md)',
                      backgroundColor: 'var(--color-bg-muted)',
                      border: '1px solid var(--color-border-primary)',
                      fontSize: '13px',
                      color: 'var(--color-text-primary)',
                    }}
                  >
                    {modName}
                  </div>
                ))
              )}
            </div>
          </div>

          <div
            style={{
              padding: '10px 14px',
              backgroundColor: 'var(--color-bg-muted)',
              borderRadius: 'var(--radius-md)',
              fontSize: '12px',
              color: 'var(--color-text-secondary)',
              lineHeight: 1.6,
              marginBottom: '1rem',
            }}
          >
            <strong>💡 解除与删除操作指引：</strong>
            <ol style={{ margin: '4px 0 0 0', paddingLeft: '20px' }}>
              <li>点击上方【前往解除依赖 →】按钮，跳转至对应消费模块；</li>
              <li>在消费模块的【所需依赖服务列表 (DEPEND)】中，点击【解除】移除该依赖；</li>
              <li>返回当前模块，此时该服务契约即可正常点击【删除】。</li>
            </ol>
          </div>

          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setBlockedService(null)}
            >
              我知道了
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default ModuleContractDetailPane;
