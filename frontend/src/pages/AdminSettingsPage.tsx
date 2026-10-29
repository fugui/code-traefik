import React, { useCallback, useEffect, useState } from 'react';
import { Save, ShieldAlert } from 'lucide-react';
import { EmptyState, useToast } from '@code/common';
import { apiRequest } from '../api/client';
import { ADMIN_ROLE } from '../constants';
import { useModuleUser } from '../context/ModuleUserContext';
import type { ModuleSettingItem, ModuleSettingsResponse } from '../types';

export const AdminSettingsPage: React.FC = () => {
  const { user, loadingUser } = useModuleUser();
  const { showToast } = useToast();

  const [settings, setSettings] = useState<ModuleSettingItem[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [saving, setSaving] = useState<boolean>(false);

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
          <h1 className="code-tpl-page-title">模块参数配置</h1>
          <p className="code-tpl-page-subtitle">
            模块治理样板页：演示前后端双重 RBAC 权限校验（menu.ts adminOnly + 后端 RequireAdmin）及 P1 级配置变更审计。
          </p>
        </div>
      </div>

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
    </div>
  );
};

export default AdminSettingsPage;
