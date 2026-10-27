import React, { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { Activity, Archive, CheckCircle2, FileEdit, Layers, RefreshCw, Settings, ShieldCheck } from 'lucide-react';
import { appNavigatePath, StatusTag } from '@code/common';
import { apiRequest } from '../api/client';
import { ADMIN_ROLE, MODULE_BASE_PATH, MODULE_KEY, MODULE_SUBTITLE, MODULE_TITLE } from '../constants';
import { useModuleUser } from '../context/ModuleUserContext';
import type { HealthStatusResponse, ItemStatsResponse } from '../types';

export const DashboardPage: React.FC = () => {
  const navigate = useNavigate();
  const { user, isEmbedded } = useModuleUser();
  const [stats, setStats] = useState<ItemStatsResponse>({
    total: 0,
    active: 0,
    draft: 0,
    archived: 0,
  });
  const [health, setHealth] = useState<HealthStatusResponse | null>(null);
  const [loading, setLoading] = useState<boolean>(true);

  const fetchOverview = useCallback(async () => {
    setLoading(true);
    try {
      const [statsData, healthData] = await Promise.all([
        apiRequest<ItemStatsResponse>('/items/stats'),
        apiRequest<HealthStatusResponse>('/health'),
      ]);
      setStats(statsData);
      setHealth(healthData);
    } catch {
      // 探针或统计读取异常时保持默认兜底值
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchOverview();
  }, [fetchOverview]);

  const goItems = () => {
    navigate(appNavigatePath('/items', MODULE_BASE_PATH));
  };

  const goAdminSettings = () => {
    navigate(appNavigatePath('/admin/settings', MODULE_BASE_PATH));
  };

  return (
    <div className="code-tpl-workspace">
      <div className="code-tpl-page-header">
        <div>
          <h1 className="code-tpl-page-title">{MODULE_TITLE} · 概览看板</h1>
          <p className="code-tpl-page-subtitle">{MODULE_SUBTITLE}</p>
        </div>
        <div className="code-tpl-page-actions">
          <button type="button" className="btn btn-secondary" onClick={() => void fetchOverview()} disabled={loading}>
            <RefreshCw size={15} />
            <span>刷新状态</span>
          </button>
          <button type="button" className="btn btn-primary" onClick={goItems}>
            <Layers size={15} />
            <span>进入条目管理</span>
          </button>
        </div>
      </div>

      {/* 顶部四宫格统计卡片（直接复用 @code/common 的 .code-stat-card 规范样式） */}
      <div className="grid-cols-4 gap-md">
        <div className="code-stat-card">
          <div className="code-stat-icon code-tpl-stat-icon--primary">
            <Layers size={22} />
          </div>
          <div>
            <div className="code-stat-label">业务条目总数</div>
            <div className="code-stat-value">{stats.total}</div>
          </div>
        </div>

        <div className="code-stat-card">
          <div className="code-stat-icon code-tpl-stat-icon--success">
            <CheckCircle2 size={22} />
          </div>
          <div>
            <div className="code-stat-label">生效中 (Active)</div>
            <div className="code-stat-value">{stats.active}</div>
          </div>
        </div>

        <div className="code-stat-card">
          <div className="code-stat-icon code-tpl-stat-icon--warning">
            <FileEdit size={22} />
          </div>
          <div>
            <div className="code-stat-label">草稿待审 (Draft)</div>
            <div className="code-stat-value">{stats.draft}</div>
          </div>
        </div>

        <div className="code-stat-card">
          <div className="code-stat-icon code-tpl-stat-icon--purple">
            <Archive size={22} />
          </div>
          <div>
            <div className="code-stat-label">已归档 (Archived)</div>
            <div className="code-stat-value">{stats.archived}</div>
          </div>
        </div>
      </div>

      {/* 双栏诊断与鉴权上下文面板 */}
      <div className="grid-cols-2 gap-md">
        <section className="code-card code-tpl-panel">
          <h2 className="code-tpl-panel-title">
            <Activity size={18} />
            <span>微前端容器与服务诊断</span>
          </h2>
          <div className="code-tpl-kv-list">
            <div className="code-tpl-kv-item">
              <span className="code-tpl-kv-label">当前前端运行模式</span>
              <span className="code-tpl-kv-value">
                {isEmbedded ? (
                  <StatusTag status="success" text="CodeBench 门户嵌入模式" size="sm" />
                ) : (
                  <StatusTag status="running" text="Standalone 独立调试模式" size="sm" />
                )}
              </span>
            </div>
            <div className="code-tpl-kv-item">
              <span className="code-tpl-kv-label">模块联邦标识 (Module Key)</span>
              <span className="code-tpl-kv-value">
                <code className="code-tpl-code-inline">{MODULE_KEY}</code>
              </span>
            </div>
            <div className="code-tpl-kv-item">
              <span className="code-tpl-kv-label">后端健康探针 (/api/health)</span>
              <span className="code-tpl-kv-value">
                {health?.status === 'healthy' ? (
                  <StatusTag status="success" text={`正常 (${health.version})`} size="sm" />
                ) : (
                  <StatusTag status="warning" text="待连接或探活中" size="sm" />
                )}
              </span>
            </div>
            <div className="code-tpl-kv-item">
              <span className="code-tpl-kv-label">联邦暴露入口契约</span>
              <span className="code-tpl-kv-value">
                <code className="code-tpl-code-inline">{`${MODULE_BASE_PATH}/assets/remoteEntry.js`}</code>
              </span>
            </div>
          </div>
        </section>

        <section className="code-card code-tpl-panel">
          <h2 className="code-tpl-panel-title">
            <ShieldCheck size={18} />
            <span>统一身份与 RBAC 角色上下文</span>
          </h2>
          <div className="code-tpl-kv-list">
            <div className="code-tpl-kv-item">
              <span className="code-tpl-kv-label">当前登录成员</span>
              <span className="code-tpl-kv-value">{user?.name || user?.username || '未识别'}</span>
            </div>
            <div className="code-tpl-kv-item">
              <span className="code-tpl-kv-label">所属组织部门</span>
              <span className="code-tpl-kv-value">{user?.department_name || '未绑定部门'}</span>
            </div>
            <div className="code-tpl-kv-item">
              <span className="code-tpl-kv-label">模块管理员角色要求</span>
              <span className="code-tpl-kv-value">
                <code className="code-tpl-code-inline">{ADMIN_ROLE}</code>
              </span>
            </div>
            <div className="code-tpl-kv-item">
              <span className="code-tpl-kv-label">当前权限判定</span>
              <span className="code-tpl-kv-value">
                {user?.is_super_admin ? (
                  <span className="code-badge code-badge--purple">超级管理员 (super_admin)</span>
                ) : user?.is_module_admin ? (
                  <span className="code-badge code-badge--primary">模块管理员 ({ADMIN_ROLE})</span>
                ) : (
                  <span className="code-badge code-badge--muted">普通成员</span>
                )}
              </span>
            </div>
          </div>

          {user?.is_module_admin && (
            <div className="flex-between">
              <span className="text-secondary">您具备模块治理权限，可配置本模块的全局运行参数。</span>
              <button type="button" className="btn btn-outline btn-small" onClick={goAdminSettings}>
                <Settings size={14} />
                <span>模块参数配置</span>
              </button>
            </div>
          )}
        </section>
      </div>
    </div>
  );
};

export default DashboardPage;
