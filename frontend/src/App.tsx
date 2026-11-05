import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import {
  LayoutDashboard,
  Radio,
  Bug,
  RotateCcw,
  Network,
  Layers,
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
import { resolveModulePath } from './hooks/useModulePath';
import { traefikMenuConfig } from './menu';
import DashboardWorkbenchPage from './pages/DashboardWorkbenchPage';
import RecordingWorkbenchPage from './pages/RecordingWorkbenchPage';
import TrafficDebugWorkbenchPage from './pages/TrafficDebugWorkbenchPage';
import ReplayWorkbenchPage from './pages/ReplayWorkbenchPage';
import TopologyWorkbenchPage from './pages/TopologyWorkbenchPage';
import ArchitectureWorkbenchPage from './pages/ArchitectureWorkbenchPage';
import ProtoSchemaWorkbenchPage from './pages/ProtoSchemaWorkbenchPage';
import AgentManagementPage from './pages/AgentManagementPage';
import AdminSettingsPage from './pages/AdminSettingsPage';
import type { CurrentUserProfile } from './types';
import './index.css';

// 安装全局 fetch 拦截器：在微前端嵌入模式下自动将 /api/* 重写为 /<MODULE_KEY>/api/* 并注入 Bearer Token
if (typeof window !== 'undefined') {
  setupFetchInterceptor({ appPrefix: MODULE_BASE_PATH });
}

export interface AppProps {
  /** CodeBench 宿主动态挂载时会自动注入 isEmbedded={true} */
  isEmbedded?: boolean;
}

/**
 * 历史路由无损重定向组件：完整保留所有 Query 参数，平滑跳转至新路由
 */
export const LegacyRouteRedirect: React.FC<{ to: string }> = ({ to }) => {
  const location = useLocation();
  const search = location.search || '';
  const target = `${to}${search}`;
  return (
    <>
      <div data-testid="legacy-redirect" data-target={target} style={{ display: 'none' }} />
      <Navigate to={target} replace />
    </>
  );
};

/**
 * /architecture 路由包装器：
 * 当携带 ?tab=topology|physical|verify 时无损重定向至 /topology，
 * 其中 tab=physical 自动映射为 view=PHYSICAL，tab=topology 映射为 view=LOGICAL；
 * 若未携带上述 tab 参数，则渲染瘦身后的纯逻辑架构元素工作台。
 */
export const ArchitectureRouteWrapper: React.FC<{ isEmbedded?: boolean }> = ({ isEmbedded }) => {
  const location = useLocation();
  const searchParams = new URLSearchParams(location.search);
  const tab = searchParams.get('tab');
  if (tab === 'topology' || tab === 'physical' || tab === 'verify') {
    const nextParams = new URLSearchParams(location.search);
    nextParams.delete('tab');
    if (tab === 'physical') {
      nextParams.set('view', 'PHYSICAL');
    } else if (tab === 'topology') {
      nextParams.set('view', 'LOGICAL');
    }
    const qs = nextParams.toString();
    const target = resolveModulePath(`/topology${qs ? `?${qs}` : ''}`, Boolean(isEmbedded));
    return (
      <>
        <div data-testid="legacy-redirect" data-target={target} style={{ display: 'none' }} />
        <Navigate to={target} replace />
      </>
    );
  }
  return <ArchitectureWorkbenchPage />;
};

const renderNavIcon = (path: string) => {
  switch (path) {
    case '/dashboard':
      return <LayoutDashboard size={16} />;
    case '/record':
      return <Radio size={16} />;
    case '/debug':
      return <Bug size={16} />;
    case '/replay':
      return <RotateCcw size={16} />;
    case '/topology':
      return <Network size={16} />;
    case '/architecture':
      return <Layers size={16} />;
    case '/schemas':
      return <FileCode size={16} />;
    case '/agents':
      return <Server size={16} />;
    case '/admin/settings':
      return <Settings size={16} />;
    default:
      return <Radio size={16} />;
  }
};

/**
 * 嵌入模式路由视图：无独立侧边栏与顶栏，同时兼容相对子路径与带模块前缀的完整路径
 */
export const EmbeddedRoutes: React.FC = () => {
  const defaultTarget = `${MODULE_BASE_PATH}/dashboard`;
  return (
    <Routes>
      <Route path="/" element={<LegacyRouteRedirect to={defaultTarget} />} />
      <Route path={MODULE_BASE_PATH} element={<LegacyRouteRedirect to={defaultTarget} />} />
      <Route path={`${MODULE_BASE_PATH}/`} element={<LegacyRouteRedirect to={defaultTarget} />} />

      {/* 4 组 9 项置顶概览大盘 */}
      <Route path="/dashboard" element={<DashboardWorkbenchPage />} />
      <Route path={`${MODULE_BASE_PATH}/dashboard`} element={<DashboardWorkbenchPage />} />

      {/* 历史旧路由无损兼容重定向 */}
      <Route path="/sessions" element={<LegacyRouteRedirect to={`${MODULE_BASE_PATH}/record`} />} />
      <Route path={`${MODULE_BASE_PATH}/sessions`} element={<LegacyRouteRedirect to={`${MODULE_BASE_PATH}/record`} />} />
      <Route path="/traffic" element={<LegacyRouteRedirect to={`${MODULE_BASE_PATH}/debug`} />} />
      <Route path={`${MODULE_BASE_PATH}/traffic`} element={<LegacyRouteRedirect to={`${MODULE_BASE_PATH}/debug`} />} />

      {/* 4 组 9 项业务路由 */}
      <Route path="/record" element={<RecordingWorkbenchPage />} />
      <Route path={`${MODULE_BASE_PATH}/record`} element={<RecordingWorkbenchPage />} />
      <Route path="/debug" element={<TrafficDebugWorkbenchPage />} />
      <Route path={`${MODULE_BASE_PATH}/debug`} element={<TrafficDebugWorkbenchPage />} />
      <Route path="/replay" element={<ReplayWorkbenchPage />} />
      <Route path={`${MODULE_BASE_PATH}/replay`} element={<ReplayWorkbenchPage />} />
      <Route path="/topology" element={<TopologyWorkbenchPage />} />
      <Route path={`${MODULE_BASE_PATH}/topology`} element={<TopologyWorkbenchPage />} />
      <Route path="/architecture" element={<ArchitectureRouteWrapper isEmbedded />} />
      <Route path={`${MODULE_BASE_PATH}/architecture`} element={<ArchitectureRouteWrapper isEmbedded />} />
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
 * 直接消费 traefikMenuConfig 数据驱动渲染，消除双处硬编码漂移
 */
export const StandaloneShell: React.FC<{ onLogout: () => void }> = ({ onLogout }) => {
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
  const isAdmin = Boolean(user.is_module_admin || user.is_super_admin);

  const currentMenuItem = traefikMenuConfig.groups
    .flatMap((g) => g.items)
    .find((item) => pathname === item.path || (item.path !== '/' && pathname.startsWith(item.path)));

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
          {traefikMenuConfig.groups.map((group) => {
            if (group.adminOnly && !isAdmin) return null;
            const visibleItems = group.items.filter((item) => !item.adminOnly || isAdmin);
            if (visibleItems.length === 0) return null;

            return (
              <div key={group.title} className="code-tpl-nav-group">
                <div className="code-tpl-nav-group-title">{group.title}</div>
                {visibleItems.map((item) => {
                  const isActive =
                    pathname === item.path ||
                    (item.path !== '/' && pathname.startsWith(item.path));
                  return (
                    <Link
                      key={item.path}
                      to={item.path}
                      className={`code-tpl-nav-link ${isActive ? 'code-tpl-nav-link--active' : ''}`}
                    >
                      {renderNavIcon(item.path)}
                      <span>{item.label}</span>
                    </Link>
                  );
                })}
              </div>
            );
          })}
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
              {currentMenuItem?.label || '治理大盘'}
            </span>
          </div>
        </header>

        <div className="code-tpl-content-scroll">
          <Routes>
            <Route path="/" element={<LegacyRouteRedirect to="/dashboard" />} />
            <Route path="/dashboard" element={<DashboardWorkbenchPage />} />
            <Route path="/sessions" element={<LegacyRouteRedirect to="/record" />} />
            <Route path="/record" element={<RecordingWorkbenchPage />} />
            <Route path="/traffic" element={<LegacyRouteRedirect to="/debug" />} />
            <Route path="/debug" element={<TrafficDebugWorkbenchPage />} />
            <Route path="/replay" element={<ReplayWorkbenchPage />} />
            <Route path="/topology" element={<TopologyWorkbenchPage />} />
            <Route path="/architecture" element={<ArchitectureRouteWrapper />} />
            <Route path="/schemas" element={<ProtoSchemaWorkbenchPage />} />
            <Route path="/agents" element={<AgentManagementPage />} />
            <Route path="/admin/settings" element={<AdminSettingsPage />} />
            <Route path="*" element={<Navigate to="/dashboard" replace />} />
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
