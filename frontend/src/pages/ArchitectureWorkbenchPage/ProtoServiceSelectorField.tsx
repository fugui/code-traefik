import React from 'react';
import { FileCode, Edit3 } from 'lucide-react';
import type { ClaimableProtoServiceItem, ServiceContract } from '../../types';

export type ProtoServiceInputMode = 'CLAIM' | 'MANUAL';
export type ProtoServiceSelectorIntent = 'PROVIDE' | 'CONTRACT';

export interface ProtoServiceSelectorFieldProps {
  /** 区分使用场景：业务模块声明供给服务 (PROVIDE) 或契约库登记多实现契约 (CONTRACT) */
  intent: ProtoServiceSelectorIntent;
  mode: ProtoServiceInputMode;
  onModeChange: (mode: ProtoServiceInputMode) => void;
  serviceName: string;
  onServiceNameChange: (serviceName: string) => void;
  claimableServices: ClaimableProtoServiceItem[];
  loadingClaimable: boolean;
  allContracts: ServiceContract[];
  testIdPrefix?: string;
}

/**
 * 可复用的 Service 名称双模选择器：支持从已激活 Proto Service 选择或手动预先输入声明。
 */
export const ProtoServiceSelectorField: React.FC<ProtoServiceSelectorFieldProps> = ({
  intent,
  mode,
  onModeChange,
  serviceName,
  onServiceNameChange,
  claimableServices,
  loadingClaimable,
  allContracts,
  testIdPrefix = intent === 'CONTRACT' ? 'contract' : 'provide',
}) => {
  const isContract = intent === 'CONTRACT';
  const isKnownContract = (item: ClaimableProtoServiceItem) =>
    Boolean(item.is_multi_impl) || allContracts.some((c) => c.service_name === item.service_name);

  const selectedItem = claimableServices.find((s) => s.service_name === serviceName);

  const getOptionMeta = (item: ClaimableProtoServiceItem) => {
    const knownContract = isKnownContract(item);
    if (isContract) {
      if (knownContract) {
        return {
          disabled: true,
          optionSuffix: ' [已登记为多实现契约]',
          badgeClass: 'code-badge--warning',
          badgeText: '已登记为多实现契约',
        };
      }
      if (item.is_claimed) {
        const holder = item.claimed_by_module?.name || '其他模块';
        return {
          disabled: true,
          optionSuffix: ` [已被 ${holder} 独占]`,
          badgeClass: 'code-badge--warning',
          badgeText: `已由 ${item.claimed_by_module?.name || '其他模块'} [${item.claimed_by_module?.code || '-'}] 独占（请先解绑或转换）`,
        };
      }
      return {
        disabled: false,
        optionSuffix: ' [可登记]',
        badgeClass: 'code-badge--success',
        badgeText: '可登记为多实现契约',
      };
    }

    if (knownContract) {
      return {
        disabled: false,
        optionSuffix: ' [多实现 LIB 契约 · 可添加实现]',
        badgeClass: 'code-badge--info',
        badgeText: '多实现 LIB 契约 · 可添加实现',
      };
    }
    if (item.is_claimed) {
      return {
        disabled: true,
        optionSuffix: ` [已被 ${item.claimed_by_module?.name || '其他模块'} 占用]`,
        badgeClass: 'code-badge--warning',
        badgeText: `已由 ${item.claimed_by_module?.name || '其他模块'} [${item.claimed_by_module?.code || '-'}] 占用`,
      };
    }
    return {
      disabled: false,
      optionSuffix: ' [可认领]',
      badgeClass: 'code-badge--success',
      badgeText: '可自由认领',
    };
  };

  const selectedMeta = selectedItem ? getOptionMeta(selectedItem) : null;

  return (
    <>
      <div className="code-form-item">
        <label className="code-form-label">
          {isContract ? '契约声明方式 *:' : '服务声明方式 *:'}
        </label>
        <div className="code-type-selector-tabs" data-testid={`${testIdPrefix}-mode-tabs`}>
          <button
            type="button"
            className={`code-type-selector-tab ${mode === 'CLAIM' ? 'code-type-selector-tab--active' : ''}`}
            data-testid={`${testIdPrefix}-mode-claim`}
            onClick={() => onModeChange('CLAIM')}
          >
            <FileCode size={14} className="code-type-selector-tab__icon" />
            {isContract ? '从已激活 Proto Service 选择' : '从已激活 Proto Service 认领'}
          </button>
          <button
            type="button"
            className={`code-type-selector-tab ${mode === 'MANUAL' ? 'code-type-selector-tab--active' : ''}`}
            data-testid={`${testIdPrefix}-mode-manual`}
            onClick={() => onModeChange('MANUAL')}
          >
            <Edit3 size={14} className="code-type-selector-tab__icon" />
            手动预先输入声明
          </button>
        </div>
      </div>

      {mode === 'CLAIM' ? (
        <div className="code-form-item">
          <label className="code-form-label">
            {isContract ? '选择待登记的 Proto Service *:' : '选择待认领的 Proto Service *:'}
          </label>
          {loadingClaimable ? (
            <div className="code-color-muted code-text-sm">正在加载全系统已激活 Proto 服务...</div>
          ) : (
            <select
              className="code-select"
              data-testid={`${testIdPrefix}-proto-select`}
              value={serviceName}
              onChange={(e) => onServiceNameChange(e.target.value)}
            >
              <option value="">-- 请选择全系统已上传并激活的 Proto Service --</option>
              {claimableServices.map((item) => {
                const meta = getOptionMeta(item);
                return (
                  <option
                    key={item.service_name}
                    value={item.service_name}
                    disabled={meta.disabled}
                  >
                    {item.service_name} ({item.operation_count} 个方法 | Schema: {item.schema_name})
                    {meta.optionSuffix}
                  </option>
                );
              })}
            </select>
          )}
          {claimableServices.length === 0 && !loadingClaimable && (
            <span className="code-form-help code-color-warning" data-testid={`${testIdPrefix}-proto-empty-hint`}>
              当前平台暂无已上传或激活的 .pb IDL 描述集。您可以切换至「手动预先输入声明」，或先前往【Proto 描述集】工作台上传激活。
            </span>
          )}

          {selectedItem && selectedMeta && (
            <div className="code-proto-preview-card" data-testid={`${testIdPrefix}-proto-preview`}>
              <div className="code-proto-preview-card__header">
                <span className="code-proto-preview-card__title">{selectedItem.service_name}</span>
                <span className={`code-badge ${selectedMeta.badgeClass}`}>{selectedMeta.badgeText}</span>
              </div>
              <div className="code-proto-preview-card__meta">
                所属 Schema: <strong>{selectedItem.schema_name}</strong> | 包名:{' '}
                <code>{selectedItem.proto_package || '(无)'}</code> | 包含方法数:{' '}
                <strong>{selectedItem.operation_count}</strong>
              </div>
              {selectedItem.operations.length > 0 && (
                <div className="code-proto-preview-card__ops">
                  {selectedItem.operations.map((op) => (
                    <span key={op} className="code-proto-preview-card__op-chip">
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
          <label className="code-form-label">
            {isContract ? '服务契约名称 (service_name) *:' : '服务名称 (service_name) *:'}
          </label>
          <input
            type="text"
            className="code-input"
            data-testid={isContract ? 'contract-service-name-input' : 'provide-service-name-input'}
            value={serviceName}
            onChange={(e) => onServiceNameChange(e.target.value)}
            placeholder={
              isContract
                ? '建议格式: {package}.{serviceName}，如 dophi.payment.v1.PayService'
                : '建议格式: {package}.{serviceName}，如 dophi.order.v1.TradeService'
            }
          />
          <span className="code-form-help">
            {isContract
              ? '全平台唯一的服务契约 IDL 标识，对应 .pb 中的点分服务规范（支持未上传 .pb 前预先登记）'
              : '全平台全局唯一的服务契约名称，遵循点分命名规范，其他 FM 或网关将基于此服务名发起调用'}
          </span>
        </div>
      )}
    </>
  );
};
