import React, { useState, useEffect } from 'react';
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
import { Drawer } from '@code/common';
import * as archApi from '../../api/arch';
import type {
  Module,
  ModuleService,
  ModuleDependency,
  AvailableServiceItem,
  ClaimableProtoServiceItem,
  Subsystem,
  FunctionalGroupTreeNode,
  Process,
} from '../../types';

export interface ModuleContractDetailPaneProps {
  modules: Module[];
  selectedModuleId: string;
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
  onCreateService: (data: {
    service_name: string;
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
    description?: string;
  }) => Promise<void>;
  onDeleteDependency: (depId: string) => Promise<void>;
  onBindProcess: (moduleId: string, processId: string | null) => Promise<void>;
}

export const ModuleContractDetailPane: React.FC<ModuleContractDetailPaneProps> = ({
  modules,
  selectedModuleId,
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
  onCreateService,
  onUpdateServiceIgnorePaths,
  onDeleteService,
  onCreateDependency,
  onDeleteDependency,
  onBindProcess,
}) => {
  // 内联编辑忽略路径状态
  const [editingServiceId, setEditingServiceId] = useState<string | null>(null);
  const [ignorePathInput, setIgnorePathInput] = useState<string>('');

  // 1. 声明供给服务 Drawer
  const [isProvideDrawerOpen, setIsProvideDrawerOpen] = useState(false);
  const [provideSubmitting, setProvideSubmitting] = useState(false);
  const [provideMode, setProvideMode] = useState<'CLAIM' | 'MANUAL'>('CLAIM');
  const [claimableServices, setClaimableServices] = useState<ClaimableProtoServiceItem[]>([]);
  const [loadingClaimable, setLoadingClaimable] = useState(false);
  const [provideForm, setProvideForm] = useState({
    service_name: '',
    diff_ignore_paths: '',
    description: '',
  });

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
    }
  }, [isProvideDrawerOpen]);

  const selectedClaimableItem = claimableServices.find(
    (s) => s.service_name === provideForm.service_name
  );

  // 2. 添加服务依赖 Drawer
  const [isDependDrawerOpen, setIsDependDrawerOpen] = useState(false);
  const [dependSubmitting, setDependSubmitting] = useState(false);
  const [dependType, setDependType] = useState<'INTERNAL' | 'EXTERNAL'>('INTERNAL');
  const [dependForm, setDependForm] = useState({
    service_id: '',
    external_system: '',
    external_service: '',
    description: '',
  });
  const [availableServices, setAvailableServices] = useState<AvailableServiceItem[]>([]);
  const [loadingAvailable, setLoadingAvailable] = useState(false);

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
    if (provideMode === 'CLAIM' && selectedClaimableItem?.is_claimed) return;
    setProvideSubmitting(true);
    try {
      const paths = provideForm.diff_ignore_paths
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      await onCreateService({
        service_name: provideForm.service_name.trim(),
        diff_ignore_paths: paths,
        description: provideForm.description.trim(),
      });
      setIsProvideDrawerOpen(false);
      setProvideForm({ service_name: '', diff_ignore_paths: '', description: '' });
      setProvideMode('CLAIM');
    } finally {
      setProvideSubmitting(false);
    }
  };

  // 提交依赖服务
  const handleSubmitDepend = async () => {
    setDependSubmitting(true);
    try {
      if (dependType === 'INTERNAL') {
        if (!dependForm.service_id) return;
        await onCreateDependency({
          is_external: false,
          service_id: dependForm.service_id,
          description: dependForm.description.trim(),
        });
      } else {
        if (!dependForm.external_system.trim() || !dependForm.external_service.trim()) return;
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
    if (window.confirm(`确认解除功能模块 [${selectedModule.name}] 与物理进程的装载关联？`)) {
      await onBindProcess(selectedModule.id, null);
    }
  };

  return (
    <div className="code-module-pane" data-testid="module-contract-detail-pane">
      {/* 顶部面包屑路径 */}
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
            <span className="code-module-breadcrumb__item code-module-breadcrumb__item--current">
              {currentGroup?.name || selectedModule?.group?.name}
            </span>
          </>
        )}
      </div>

      {/* 模块选择与概览卡片 */}
      <div className="code-module-pane__header">
        <div className="code-module-pane__title-row">
          <span className="code-module-pane__title">功能模块契约与进程绑定</span>
          {isAdmin && (
            <button
              type="button"
              className="code-btn code-btn--primary code-btn--xs"
              onClick={onCreateModule}
            >
              <Plus size={13} /> 新建模块
            </button>
          )}
        </div>

        <div className="code-module-selector-tabs">
          {(modules ?? []).map((m) => (
            <button
              key={m.id}
              type="button"
              className={`code-module-tab-chip ${selectedModuleId === m.id ? 'code-module-tab-chip--active' : ''}`}
              onClick={() => onSelectModule(m.id)}
            >
              <span className="code-module-chip__code">{m.code}</span>
              <span className="code-module-chip__name">{m.name}</span>
            </button>
          ))}
          {(modules ?? []).length === 0 && (
            <div className="code-empty-pane code-p-sm">当前功能组下暂无功能模块</div>
          )}
        </div>
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
                  {selectedModule.process ? (
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

              {/* 区块 2：物理进程装载全貌表单 */}
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
                    <span className="code-module-form-field__label">承载进程:</span>
                    <span className="code-module-form-field__val">
                      {selectedModule.process ? (
                        <span
                          className="code-process-link"
                          onClick={() => {
                            if (onViewProcess && selectedModule.process?.id) {
                              onViewProcess(selectedModule.process.id);
                            }
                          }}
                          title={onViewProcess ? '点击在物理架构树中定位此进程' : undefined}
                        >
                          <Server size={14} />
                          {selectedModule.process.name}
                        </span>
                      ) : (
                        <span className="code-color-muted">未装载进物理进程</span>
                      )}
                    </span>
                  </div>
                  <div className="code-module-form-field">
                    <span className="code-module-form-field__label">监听端口:</span>
                    <span className="code-module-form-field__val code-font-mono">
                      {selectedModule.process ? (
                        <span className="code-badge code-badge--info">Port: {selectedModule.process.listen_port}</span>
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
            </div>
          </div>

          {/* 下半部分：上下两个独立的接口依赖表格 */}

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
                    setProvideForm({ service_name: '', diff_ignore_paths: '', description: '' });
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
                    <th style={{ width: '15%' }}>下游依赖方数量</th>
                    <th style={{ width: '15%' }}>说明</th>
                    {isAdmin && <th style={{ width: '10%' }}>操作</th>}
                  </tr>
                </thead>
                <tbody>
                  {services.map((svc) => (
                    <tr key={svc.id}>
                      <td>
                        <span className="code-font-mono code-color-primary" style={{ fontWeight: 600 }}>
                          {svc.service_name}
                        </span>
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
                        <span className="code-badge code-badge--info">
                          {svc.downstream_count || 0} 个依赖方
                        </span>
                      </td>
                      <td>{svc.description || '-'}</td>
                      {isAdmin && (
                        <td>
                          <button
                            type="button"
                            className="code-btn code-btn--danger code-btn--xs"
                            onClick={() => {
                              if ((svc.downstream_count || 0) > 0) {
                                alert(`无法删除：该服务正被 ${svc.downstream_count} 个下游模块依赖，请先在下游模块中解除依赖！`);
                                return;
                              }
                              if (window.confirm(`确认删除对外服务契约 [${svc.service_name}]？`)) {
                                void onDeleteService(svc.id);
                              }
                            }}
                          >
                            删除
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

          {/* 下表格：依赖下游服务列表 (DEPEND Services) */}
          <div className="code-service-section">
            <div className="code-service-section-header">
              <div className="code-service-section-title-wrap">
                <div className="code-service-section-title-row">
                  <h4 className="code-service-section-title">
                    依赖下游服务列表 (DEPEND)
                  </h4>
                  <span className="code-badge code-badge--info">{dependencies.length} 个依赖</span>
                </div>
                <p className="code-service-section-desc">
                  当前模块运行所依赖的下游 Service 服务契约（支持平台内已有模块供给服务或外部第三方系统）
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
                    const proc = providerMod?.process;

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
                                if (onViewProcess && proc.id) onViewProcess(proc.id);
                              }}
                              title={onViewProcess ? '在物理架构树中定位此承载进程' : undefined}
                            >
                              <Server size={13} />
                              {proc.name} (Port: {proc.listen_port})
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
                                if (window.confirm(`确认解除对服务 [${svcName}] 的依赖？`)) {
                                  void onDeleteDependency(dep.id);
                                }
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
                        当前模块暂未声明任何下游服务依赖，点击右上角【+ 添加服务依赖】声明链路
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      ) : (
        <div className="code-empty-pane code-p-lg">请先在左侧选择或新建一个功能模块</div>
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
                (provideMode === 'CLAIM' && selectedClaimableItem?.is_claimed)
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
        title={`添加下游服务依赖 [${selectedModule?.name || ''}]`}
        subtitle="声明当前功能模块在业务链路中需要调用或依赖的下游 Service"
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
                  <option value="">-- 请选择被依赖的下游 Service --</option>
                  {availableServices.map((item) => (
                    <option key={item.service_id} value={item.service_id}>
                      {item.service_name} (供给方: {item.module_name} [{item.module_code}] / 子系统: {item.subsystem_name})
                    </option>
                  ))}
                </select>
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
                  {p.name} (Port: {p.listen_port}) [{p.group_name || '默认组'}] - 宿主: {p.agent?.name || `Agent #${p.agent_id}`}
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
                      <span className="code-badge code-badge--info">Port: {targetProc.listen_port}</span>
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
    </div>
  );
};

export default ModuleContractDetailPane;
