import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import {
  Network,
  Activity,
  RotateCcw,
  FileCode,
  Server,
  Settings,
  LogOut,
  Moon,
  Sun,
} from 'lucide-react';
import {
  AUTH_TOKEN_KEY,
  ConfirmProvider,
  ErrorBoundary,
  ToastProvider,
  UnifiedLogin,
  setupFetchInterceptor,
  useTheme,
} from '@code/common';
import { apiRequest } from './api/client';
import {
  MODULE_BASE_PATH,
  MODULE_DESC,
  MODULE_KEY,
  MODULE_SUBTITLE,
  MODULE_TITLE,
} from './constants';
import { ModuleUserProvider, useModuleUser } from './context/ModuleUserContext';
import ArchitectureWorkbenchPage from './pages/ArchitectureWorkbenchPage';
import TrafficMonitorWorkbenchPage from './pages/TrafficMonitorWorkbenchPage';
import ReplayWorkbenchPage from './pages/ReplayWorkbenchPage';
import ProtoSchemaWorkbenchPage from './pages/ProtoSchemaWorkbenchPage';
import AgentManagementPage from './pages/AgentManagementPage';
import AdminSettingsPage from './pages/AdminSettingsPage';
import type { CurrentUserProfile } from './types';
import './index.css';

// 安装全局 fetch 拦截器：在微前端嵌入模式下自动将 /api/* 重写为 /<MODULE_KEY>/api/* 并注入 Bearer Token
setupFetchInterceptor({ appPrefix: MODULE_BASE_PATH });

export interface AppProps {
  /** CodeBench 宿主动态挂载时会自动注入 isEmbedded={true} */
  isEmbedded?: boolean;
}

/**
 * 嵌入模式路由视图：无独立侧边栏与顶栏，同时兼容相对子路径与带模块前缀的完整路径
 */
const EmbeddedRoutes: React.FC = () => {
  const defaultTarget = `${MODULE_BASE_PATH}/architecture`;
  return (
    <Routes>
      <Route path="/" element={<Navigate to={defaultTarget} replace />} />
      <Route path={MODULE_BASE_PATH} element={<Navigate to={defaultTarget} replace />} />
      <Route path={`${MODULE_BASE_PATH}/`} element={<Navigate to={defaultTarget} replace />} />
      <Route path="/architecture" element={<ArchitectureWorkbenchPage />} />
      <Route path={`${MODULE_BASE_PATH}/architecture`} element={<ArchitectureWorkbenchPage />} />
      <Route path="/traffic" element={<TrafficMonitorWorkbenchPage />} />
      <Route path={`${MODULE_BASE_PATH}/traffic`} element={<TrafficMonitorWorkbenchPage />} />
      <Route path="/replay" element={<ReplayWorkbenchPage />} />
      <Route path={`${MODULE_BASE_PATH}/replay`} element={<ReplayWorkbenchPage />} />
      <Route path="/schemas" element={<ProtoSchemaWorkbenchPage />} />
      <Route path={`${MODULE_BASE_PATH}/schemas`} element={<ProtoSchemaWorkbenchPage />} />
      <Route path="/agents" element={<AgentManagementPage />} />
      <Route path={`${MODULE_BASE_PATH}/agents`} element={<AgentManagementPage />} />
      <Route path="/admin/settings" element={<AdminSettingsPage />} />
      <Route path={`${MODULE_BASE_PATH}/admin/settings`} element={<AdminSettingsPage />} />
      <Route path="*" element={<Navigate to={defaultTarget} replace />} />
    </Routes>
  );
};

/**
 * 独立调试模式布局外壳：自带简易导航、主题明暗切换器与本地登录守卫
 */
const StandaloneShell: React.FC<{ onLogout: () => void }> = ({ onLogout }) => {
  const location = useLocation();
  const { theme, toggleTheme } = useTheme('light');
  const { user, loadingUser, reloadUser } = useModuleUser();

  if (loadingUser) {
    return <div className="code-tpl-loading-screen">正在校验登录会话状态...</div>;
  }

  if (!user) {
    return (
      <UnifiedLogin
        systemName={`${MODULE_TITLE} (${MODULE_KEY})`}
        systemSubtitle={MODULE_SUBTITLE}
        systemDesc={MODULE_DESC}
        onLoginSuccess={() => {
          void reloadUser();
        }}
      />
    );
  }

  const pathname = location.pathname;
  const isArch = pathname === '/' || pathname.startsWith('/architecture');
  const isTraffic = pathname.startsWith('/traffic');
  const isReplay = pathname.startsWith('/replay');
  const isSchemas = pathname.startsWith('/schemas');
  const isAgents = pathname.startsWith('/agents');
  const isSettings = pathname.startsWith('/admin/settings');
  const isAdmin = Boolean(user.is_module_admin || user.is_super_admin);

  return (
    <div className="code-tpl-standalone-shell">
      <aside className="code-tpl-sidebar">
        <div className="code-tpl-sidebar-brand">
          <div className="code-tpl-brand-logo">{MODULE_KEY.slice(0, 1).toUpperCase()}</div>
          <div className="code-tpl-brand-meta">
            <span className="code-tpl-brand-title">{MODULE_TITLE}</span>
            <span className="code-tpl-brand-badge">独立开发调试模式</span>
          </div>
        </div>

        <nav className="code-tpl-nav">
          <div className="code-tpl-nav-group">
            <div className="code-tpl-nav-group-title">流量与架构</div>
            <Link
              to="/architecture"
              className={`code-tpl-nav-link ${isArch ? 'code-tpl-nav-link--active' : ''}`}
            >
              <Network size={16} />
              <span>架构中心与拓扑</span>
            </Link>
            <Link
              to="/traffic"
              className={`code-tpl-nav-link ${isTraffic ? 'code-tpl-nav-link--active' : ''}`}
            >
              <Activity size={16} />
              <span>协同录制与流量解剖</span>
            </Link>
            <Link
              to="/replay"
              className={`code-tpl-nav-link ${isReplay ? 'code-tpl-nav-link--active' : ''}`}
            >
              <RotateCcw size={16} />
              <span>跨Agent重放与回归</span>
            </Link>
            <Link
              to="/schemas"
              className={`code-tpl-nav-link ${isSchemas ? 'code-tpl-nav-link--active' : ''}`}
            >
              <FileCode size={16} />
              <span>Proto描述集工作台</span>
            </Link>
          </div>

          <div className="code-tpl-nav-group">
            <div className="code-tpl-nav-group-title">资源与治理</div>
            <Link
              to="/agents"
              className={`code-tpl-nav-link ${isAgents ? 'code-tpl-nav-link--active' : ''}`}
            >
              <Server size={16} />
              <span>Agent纳管与租约</span>
            </Link>
            {isAdmin && (
              <Link
                to="/admin/settings"
                className={`code-tpl-nav-link ${isSettings ? 'code-tpl-nav-link--active' : ''}`}
              >
                <Settings size={16} />
                <span>模块参数配置</span>
              </Link>
            )}
          </div>
        </nav>

        <div className="code-tpl-sidebar-footer">
          <div className="code-tpl-user-summary">
            <span className="code-tpl-user-name">{user.name || user.username}</span>
            <span className="code-tpl-user-role">
              {isAdmin ? '模块管理员' : '普通研发成员'}
            </span>
          </div>
          <div className="code-tpl-footer-actions">
            <button
              type="button"
              className="code-tpl-btn-icon"
              onClick={toggleTheme}
              title={`切换至${theme === 'dark' ? '明亮' : '暗色'}主题`}
            >
              {theme === 'dark' ? <Sun size={15} /> : <Moon size={15} />}
            </button>
            <button
              type="button"
              className="code-tpl-btn-icon code-tpl-btn-icon--danger"
              onClick={onLogout}
              title="退出登录"
            >
              <LogOut size={15} />
            </button>
          </div>
        </div>
      </aside>

      <main className="code-tpl-standalone-main">
        <header className="code-tpl-topbar">
          <div className="code-tpl-topbar-breadcrumb">
            <span className="code-tpl-breadcrumb-parent">{MODULE_TITLE}</span>
            <span className="code-tpl-breadcrumb-sep">/</span>
            <span className="code-tpl-breadcrumb-current">
              {isArch && '架构中心与拓扑'}
              {isTraffic && '协同录制与流量解剖'}
              {isReplay && '跨Agent重放与回归'}
              {isSchemas && 'Proto描述集工作台'}
              {isAgents && 'Agent纳管与租约'}
              {isSettings && '模块参数配置'}
            </span>
          </div>
        </header>

        <div className="code-tpl-content-scroll">
          <Routes>
            <Route path="/" element={<Navigate to="/architecture" replace />} />
            <Route path="/architecture" element={<ArchitectureWorkbenchPage />} />
            <Route path="/traffic" element={<TrafficMonitorWorkbenchPage />} />
            <Route path="/replay" element={<ReplayWorkbenchPage />} />
            <Route path="/schemas" element={<ProtoSchemaWorkbenchPage />} />
            <Route path="/agents" element={<AgentManagementPage />} />
            <Route path="/admin/settings" element={<AdminSettingsPage />} />
            <Route path="*" element={<Navigate to="/architecture" replace />} />
          </Routes>
        </div>
      </main>
    </div>
  );
};

export const App: React.FC<AppProps> = ({ isEmbedded = false }) => {
  const [currentUser, setCurrentUser] = useState<CurrentUserProfile | null>(null);
  const [loadingUser, setLoadingUser] = useState<boolean>(true);

  const fetchCurrentUser = useCallback(async () => {
    const token = localStorage.getItem(AUTH_TOKEN_KEY);
    if (!token) {
      setCurrentUser(null);
      setLoadingUser(false);
      return;
    }

    try {
      setLoadingUser(true);
      const profile = await apiRequest<CurrentUserProfile>('/me');
      setCurrentUser(profile);
    } catch {
      localStorage.removeItem(AUTH_TOKEN_KEY);
      setCurrentUser(null);
    } finally {
      setLoadingUser(false);
    }
  }, []);

  useEffect(() => {
    void fetchCurrentUser();
  }, [fetchCurrentUser]);

  const handleLogout = useCallback(() => {
    localStorage.removeItem(AUTH_TOKEN_KEY);
    setCurrentUser(null);
  }, []);

  const contextValue = useMemo(
    () => ({
      user: currentUser,
      loadingUser,
      isEmbedded,
      reloadUser: fetchCurrentUser,
    }),
    [currentUser, loadingUser, isEmbedded, fetchCurrentUser]
  );

  const innerContent = isEmbedded ? (
    <EmbeddedRoutes />
  ) : (
    <StandaloneShell onLogout={handleLogout} />
  );

  const appBody = (
    <ErrorBoundary>
      <ModuleUserProvider value={contextValue}>
        <ToastProvider>
          <ConfirmProvider>{innerContent}</ConfirmProvider>
        </ToastProvider>
      </ModuleUserProvider>
    </ErrorBoundary>
  );

  if (isEmbedded) {
    return appBody;
  }

  return <BrowserRouter>{appBody}</BrowserRouter>;
};

export default App;
