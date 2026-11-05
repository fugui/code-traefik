import React, { useCallback, useEffect, useState } from 'react';
import {
  AlertTriangle,
  Check,
  CheckCircle2,
  Copy,
  KeyRound,
  RefreshCw,
  Save,
  ShieldAlert,
  Trash2,
} from 'lucide-react';
import { EmptyState, Modal } from '@code/common';
import { useToast } from '../utils/toast';
import { apiRequest } from '../api/client';
import { ADMIN_ROLE } from '../constants';
import { useModuleUser } from '../context/ModuleUserContext';
import type {
  CITokenRotateResponse,
  ModuleSettingItem,
  ModuleSettingsResponse,
} from '../types';

export const AdminSettingsPage: React.FC = () => {
  const { user, loadingUser } = useModuleUser();
  const { showToast } = useToast();

  const [settings, setSettings] = useState<ModuleSettingItem[]>([]);
  const [ciTokenConfigured, setCiTokenConfigured] = useState<boolean>(false);
  const [loading, setLoading] = useState<boolean>(true);
  const [saving, setSaving] = useState<boolean>(false);

  // CI Token 对话框与动作状态
  const [showRotateModal, setShowRotateModal] = useState<boolean>(false);
  const [showRevokeModal, setShowRevokeModal] = useState<boolean>(false);
  const [rotating, setRotating] = useState<boolean>(false);
  const [revoking, setRevoking] = useState<boolean>(false);
  const [newlyGeneratedToken, setNewlyGeneratedToken] = useState<string | null>(null);
  const [copied, setCopied] = useState<boolean>(false);

  const isAdmin = Boolean(user?.is_super_admin || user?.is_module_admin);

  const loadSettings = useCallback(async () => {
    if (!isAdmin) {
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const data = await apiRequest<ModuleSettingsResponse>('/admin/settings');
      setSettings(Array.isArray(data.settings) ? data.settings : []);
      setCiTokenConfigured(Boolean(data.ci_token_configured));
    } catch (err) {
      const msg = err instanceof Error ? err.message : '读取模块参数失败';
      showToast(msg, 'error');
    } finally {
      setLoading(false);
    }
  }, [isAdmin, showToast]);

  useEffect(() => {
    if (!loadingUser) {
      void loadSettings();
    }
  }, [loadingUser, loadSettings]);

  const handleValueChange = (key: string, nextVal: string) => {
    setSettings((prev) =>
      prev.map((item) => (item.setting_key === key ? { ...item, setting_val: nextVal } : item)),
    );
  };

  const handleSaveAll = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    try {
      const resp = await apiRequest<ModuleSettingsResponse>('/admin/settings', {
        method: 'PUT',
        bodyData: { items: settings },
      });
      if (Array.isArray(resp.settings)) {
        setSettings(resp.settings);
      }
      showToast('模块运行参数已保存并记入审计日志', 'success');
    } catch (err) {
      const msg = err instanceof Error ? err.message : '保存模块参数失败';
      showToast(msg, 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleRotateToken = async () => {
    setRotating(true);
    try {
      const resp = await apiRequest<CITokenRotateResponse>('/admin/ci-token/rotate', {
        method: 'POST',
        bodyData: { description: 'GitLab/Jenkins CI Gate Token' },
      });
      setNewlyGeneratedToken(resp.ci_token);
      setCiTokenConfigured(true);
      setShowRotateModal(true);
      showToast('CI 机器凭据已生成，请立即妥善保存', 'success');
    } catch (err) {
      const msg = err instanceof Error ? err.message : '生成 CI 凭据失败';
      showToast(msg, 'error');
    } finally {
      setRotating(false);
    }
  };

  const handleRevokeToken = async () => {
    setRevoking(true);
    try {
      await apiRequest<{ message: string }>('/admin/ci-token', {
        method: 'DELETE',
      });
      setCiTokenConfigured(false);
      setShowRevokeModal(false);
      showToast('CI 机器凭据已成功吊销', 'success');
    } catch (err) {
      const msg = err instanceof Error ? err.message : '吊销 CI 凭据失败';
      showToast(msg, 'error');
    } finally {
      setRevoking(false);
    }
  };

  const handleCopyToken = async () => {
    if (!newlyGeneratedToken) return;
    try {
      await navigator.clipboard.writeText(newlyGeneratedToken);
      setCopied(true);
      showToast('Token 已复制到剪贴板', 'success');
      setTimeout(() => setCopied(false), 2000);
    } catch {
      showToast('复制失败，请手动选取复制', 'error');
    }
  };

  if (!loadingUser && !isAdmin) {
    return (
      <div className="code-tpl-workspace">
        <div className="code-card code-tpl-panel">
          <EmptyState
            type="permission"
            title="需要模块管理员权限"
            description={`当前页面受 RBAC 角色守卫保护，仅具备 super_admin 或 ${ADMIN_ROLE} 角色的成员可访问（后端路由同步挂载 RequireAdmin 拦截）。`}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="code-tpl-workspace">
      <div className="code-tpl-page-header">
        <div>
          <h1 className="code-tpl-page-title">系统参数与 CI/CD 门禁配置</h1>
          <p className="code-tpl-page-subtitle">
            模块治理与质量门禁配置：管理系统运行时动态参数以及 CI/CD 自动化流水线阻断门禁机器凭据。
          </p>
        </div>
      </div>

      {/* 卡片 1: CI/CD 自动化门禁凭据治理 */}
      <div className="code-card code-tpl-panel" style={{ marginBottom: 20 }}>
        <div className="flex-between">
          <h2 className="code-tpl-panel-title">
            <KeyRound size={18} />
            <span>CI/CD 自动化门禁凭据</span>
          </h2>
          <div>
            {ciTokenConfigured ? (
              <span
                className="code-badge code-badge--success"
                style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}
              >
                <CheckCircle2 size={12} />
                <span>已配置 (ACTIVE)</span>
              </span>
            ) : (
              <span
                className="code-badge code-badge--secondary"
                style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}
              >
                <AlertTriangle size={12} />
                <span>未配置</span>
              </span>
            )}
          </div>
        </div>

        <p className="text-secondary" style={{ marginTop: 8, fontSize: '0.875rem', lineHeight: 1.6 }}>
          用于 GitLab CI、GitHub Actions 或 Jenkins 等自动化流水线调用质量门禁阻断接口（
          <code className="code-tpl-code-inline">POST /api/gates/evaluate</code>
          ）的专用高熵机器凭据。数据库仅持久化安全加盐 SHA-256
          哈希，主控采用常量时间比较防范侧信道时序攻击。
        </p>

        <div style={{ display: 'flex', gap: 12, marginTop: 16 }}>
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => void handleRotateToken()}
            disabled={loading || rotating}
            style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}
          >
            <RefreshCw size={14} className={rotating ? 'spin' : ''} />
            <span>{rotating ? '生成中...' : ciTokenConfigured ? '轮换 CI Token' : '生成 CI Token'}</span>
          </button>
          {ciTokenConfigured && (
            <button
              type="button"
              className="btn btn-danger"
              onClick={() => setShowRevokeModal(true)}
              disabled={loading || revoking}
              style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}
            >
              <Trash2 size={14} />
              <span>吊销凭据</span>
            </button>
          )}
        </div>
      </div>

      {/* 卡片 2: 运行时动态配置项 */}
      <form className="code-card code-tpl-panel" onSubmit={(e) => void handleSaveAll(e)}>
        <div className="flex-between">
          <h2 className="code-tpl-panel-title">
            <ShieldAlert size={18} />
            <span>运行时动态配置项</span>
          </h2>
          <button type="submit" className="btn btn-primary" disabled={loading || saving || settings.length === 0}>
            <Save size={15} />
            <span>{saving ? '保存中...' : '保存全部修改'}</span>
          </button>
        </div>

        {loading ? (
          <EmptyState compact title="正在读取模块配置..." />
        ) : settings.length === 0 ? (
          <EmptyState compact title="暂无可配置参数" />
        ) : (
          <div className="code-tpl-setting-list">
            {(settings ?? []).map((item) => (
              <div key={item.setting_key} className="code-tpl-setting-row">
                <div className="code-tpl-setting-meta">
                  <div>
                    <code className="code-tpl-code-inline">{item.setting_key}</code>
                  </div>
                  <span className="text-secondary">{item.description || '模块运行参数'}</span>
                </div>
                <input
                  type="text"
                  className="code-input code-tpl-setting-input"
                  value={item.setting_val}
                  onChange={(e) => handleValueChange(item.setting_key, e.target.value)}
                  aria-label={item.setting_key}
                />
              </div>
            ))}
          </div>
        )}
      </form>

      {/* 对话框 1: 生成/轮换 CI Token 成功后展示 */}
      <Modal
        open={showRotateModal}
        onClose={() => {
          setShowRotateModal(false);
          setNewlyGeneratedToken(null);
        }}
        title="CI/CD 自动化门禁机器凭据"
        width="lg"
        footer={null}
      >
        <div style={{ padding: '8px 0' }}>
          <div
            style={{
              padding: '12px 16px',
              borderRadius: '6px',
              backgroundColor: 'var(--color-warning-subtle, rgba(234, 179, 8, 0.1))',
              border: '1px solid var(--color-warning, #eab308)',
              color: 'var(--color-warning-text, #ca8a04)',
              marginBottom: '16px',
              display: 'flex',
              alignItems: 'flex-start',
              gap: '8px',
              fontSize: '0.875rem',
            }}
          >
            <AlertTriangle size={18} style={{ flexShrink: 0, marginTop: 2 }} />
            <div>
              <strong>凭据仅展示一次，离开本页后将无法再次查看！</strong>
              <p style={{ margin: '4px 0 0 0', lineHeight: 1.5 }}>
                请立即复制并妥善保存至您的 CI/CD 流水线 Secret 变量（如 GitLab CI/CD Variables 中的{' '}
                <code className="code-tpl-code-inline">TRAEFIK_CI_TOKEN</code>
                ）。流水线请求门禁时将其填入请求头{' '}
                <code className="code-tpl-code-inline">X-Traefik-CI-Token</code>。
              </p>
            </div>
          </div>

          <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
            <input
              type="text"
              readOnly
              value={newlyGeneratedToken || ''}
              className="code-input"
              style={{ fontFamily: 'monospace', fontSize: '0.875rem', flex: 1 }}
              aria-label="生成的 CI Token"
            />
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => void handleCopyToken()}
              style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}
            >
              {copied ? <Check size={14} /> : <Copy size={14} />}
              <span>{copied ? '已复制' : '复制'}</span>
            </button>
          </div>

          <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 24 }}>
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => {
                setShowRotateModal(false);
                setNewlyGeneratedToken(null);
              }}
            >
              我已保存，关闭窗口
            </button>
          </div>
        </div>
      </Modal>

      {/* 对话框 2: 吊销 CI Token 确认 */}
      <Modal
        open={showRevokeModal}
        onClose={() => setShowRevokeModal(false)}
        title="确认吊销 CI 门禁机器凭据"
        width="md"
        footer={null}
      >
        <div style={{ padding: '8px 0' }}>
          <div
            style={{
              padding: '12px 16px',
              borderRadius: '6px',
              backgroundColor: 'var(--color-danger-subtle, rgba(239, 68, 68, 0.1))',
              border: '1px solid var(--color-danger, #ef4444)',
              color: 'var(--color-danger-text, #b91c1c)',
              marginBottom: '16px',
              display: 'flex',
              alignItems: 'flex-start',
              gap: '8px',
              fontSize: '0.875rem',
            }}
          >
            <AlertTriangle size={18} style={{ flexShrink: 0, marginTop: 2 }} />
            <div>
              <strong>吊销凭据将导致所有依赖该 Token 的 CI/CD 流水线门禁中断！</strong>
              <p style={{ margin: '4px 0 0 0', lineHeight: 1.5 }}>
                吊销后，后续所有携带此凭据的门禁评估请求（
                <code className="code-tpl-code-inline">POST /api/gates/evaluate</code>
                ）将直接被 401 Unauthorized 阻断，直到重新生成并配置新的 CI Token。
              </p>
            </div>
          </div>
          <p style={{ fontSize: '0.875rem', color: 'var(--color-text-secondary)', marginBottom: 20 }}>
            您确定要立即吊销当前生效的 CI/CD 自动化门禁凭据吗？
          </p>
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => setShowRevokeModal(false)}
              disabled={revoking}
            >
              取消
            </button>
            <button
              type="button"
              className="btn btn-danger"
              onClick={() => void handleRevokeToken()}
              disabled={revoking}
            >
              {revoking ? '吊销中...' : '确认吊销'}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default AdminSettingsPage;