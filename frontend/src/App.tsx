import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { LayoutDashboard, ListChecks, LogOut, Moon, Settings, Sun } from 'lucide-react';
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
import AdminSettingsPage from './pages/AdminSettingsPage';
import DashboardPage from './pages/DashboardPage';
import ItemListPage from './pages/ItemListPage';
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
  const defaultTarget = `${MODULE_BASE_PATH}/dashboard`;
  return (
    <Routes>
      <Route path="/" element={<Navigate to={defaultTarget} replace />} />
      <Route path={MODULE_BASE_PATH} element={<Navigate to={defaultTarget} replace />} />
      <Route path={`${MODULE_BASE_PATH}/`} element={<Navigate to={defaultTarget} replace />} />
      <Route path="/dashboard" element={<DashboardPage />} />
      <Route path={`${MODULE_BASE_PATH}/dashboard`} element={<DashboardPage />} />
      <Route path="/items" element={<ItemListPage />} />
      <Route path={`${MODULE_BASE_PATH}/items`} element={<ItemListPage />} />
      <Route path="/admin/settings" element={<AdminSettingsPage />} />
      <Route path={`${MODULE_BASE_PATH}/admin/settings`} element={<AdminSettingsPage />} />
      <Route path="*" element={<Navigate to={defaultTarget} replace />} />
    </Routes>
  );
};

/**
 * 独立调试模式布局外壳：自带简易导航、主题明暗切换器与本地登录守卫，方便脱离宿主独立开发
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
  const isDashboard = pathname === '/' || pathname.startsWith('/dashboard');
  const isItems = pathname.startsWith('/items');
  const isSettings = pathname.startsWith('/admin/settings');

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
            <div className="code-tpl-nav-group-title">核心业务</div>
            <Link
              to="/dashboard"
              className={`code-tpl-nav-link ${isDashboard ? 'code-tpl-nav-link--active' : ''}`}
            >
              <LayoutDashboard size={17} />
              <span>概览看板</span>
            </Link>
            <Link
              to="/items"
              className={`code-tpl-nav-link ${isItems ? 'code-tpl-nav-link--active' : ''}`}
            >
              <ListChecks size={17} />
              <span>示例条目管理</span>
            </Link>
          </div>

          {user.is_module_admin && (
            <div className="code-tpl-nav-group">
              <div className="code-tpl-nav-group-title">模块治理</div>
              <Link
                to="/admin/settings"
                className={`code-tpl-nav-link ${isSettings ? 'code-tpl-nav-link--active' : ''}`}
              >
                <Settings size={17} />
                <span>模块参数配置</span>
              </Link>
            </div>
          )}
        </nav>
      </aside>

      <div className="code-tpl-main-area">
        <header className="code-tpl-topbar">
          <h2 className="code-tpl-topbar-title">
            {isSettings ? '模块参数配置' : isItems ? '示例条目管理' : '概览看板'}
          </h2>
          <div className="code-tpl-topbar-actions">
            <button
              type="button"
              className="btn btn-secondary btn-small"
              onClick={toggleTheme}
              title="切换亮色 / 暗黑主题以检验双模视觉兼容性"
            >
              {theme === 'dark' ? <Sun size={14} /> : <Moon size={14} />}
              <span>{theme === 'dark' ? '切换亮色' : '切换暗黑'}</span>
            </button>

            <span className="code-badge code-badge--primary">
              {user.name || user.username}
            </span>

            <button type="button" className="btn btn-outline btn-small" onClick={onLogout}>
              <LogOut size={14} />
              <span>退出</span>
            </button>
          </div>
        </header>

        <main className="flex-1">
          <Routes>
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard" element={<DashboardPage />} />
            <Route path="/items" element={<ItemListPage />} />
            <Route path="/admin/settings" element={<AdminSettingsPage />} />
            <Route path="*" element={<Navigate to="/dashboard" replace />} />
          </Routes>
        </main>
      </div>
    </div>
  );
};

export const App: React.FC<AppProps> = ({ isEmbedded = false }) => {
  const effectiveEmbedded = isEmbedded || Boolean(window.__POWERED_BY_PORTAL__);
  const [user, setUser] = useState<CurrentUserProfile | null>(null);
  const [loadingUser, setLoadingUser] = useState<boolean>(true);

  const loadUser = useCallback(async () => {
    const token = localStorage.getItem(AUTH_TOKEN_KEY);
    if (!token) {
      setUser(null);
      setLoadingUser(false);
      return;
    }
    try {
      const profile = await apiRequest<CurrentUserProfile>('/me');
      setUser(profile);
    } catch {
      setUser(null);
    } finally {
      setLoadingUser(false);
    }
  }, []);

  useEffect(() => {
    void loadUser();
    const handleAuthChange = () => {
      void loadUser();
    };
    window.addEventListener('auth-change', handleAuthChange);
    return () => window.removeEventListener('auth-change', handleAuthChange);
  }, [loadUser]);

  const handleStandaloneLogout = useCallback(() => {
    localStorage.removeItem(AUTH_TOKEN_KEY);
    setUser(null);
    window.location.href = window.location.pathname.startsWith(MODULE_BASE_PATH)
      ? `${MODULE_BASE_PATH}/`
      : '/';
  }, []);

  const contextValue = useMemo(
    () => ({
      user,
      loadingUser,
      isEmbedded: effectiveEmbedded,
      reloadUser: loadUser,
    }),
    [user, loadingUser, effectiveEmbedded, loadUser],
  );

  // 独立运行模式下自适应识别 Vite base 路径（/<MODULE_KEY>/）或单二进制根路径（/）
  const standaloneBasename = useMemo(() => {
    if (typeof window === 'undefined') return '/';
    const p = window.location.pathname;
    if (p === MODULE_BASE_PATH || p.startsWith(`${MODULE_BASE_PATH}/`)) {
      return MODULE_BASE_PATH;
    }
    return '/';
  }, []);

  return (
    <ErrorBoundary title={`${MODULE_TITLE} 运行异常`}>
      {/* 子应用内部独立挂载 ConfirmProvider 与 ToastProvider，确保模块联邦非共享 Context 始终可用 */}
      <ConfirmProvider>
        <ToastProvider>
          <ModuleUserProvider value={contextValue}>
            {effectiveEmbedded ? (
              <EmbeddedRoutes />
            ) : (
              <BrowserRouter basename={standaloneBasename}>
                <StandaloneShell onLogout={handleStandaloneLogout} />
              </BrowserRouter>
            )}
          </ModuleUserProvider>
        </ToastProvider>
      </ConfirmProvider>
    </ErrorBoundary>
  );
};

export default App;
