import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { AdminSettingsPage } from '../pages/AdminSettingsPage';
import { ModuleUserProvider } from '../context/ModuleUserContext';
import type { CurrentUserProfile } from '../types';

describe('AdminSettingsPage CI Gate Token Governance', () => {
  const normalUser: CurrentUserProfile = {
    id: 1,
    username: 'dev_user',
    name: '研发成员',
    roles: ['developer'],
    is_super_admin: false,
    is_module_admin: false,
    required_ad_role: 'traefik_admin',
  };

  const adminUser: CurrentUserProfile = {
    id: 2,
    username: 'admin_user',
    name: '管理员',
    roles: ['traefik_admin'],
    is_super_admin: false,
    is_module_admin: true,
    required_ad_role: 'traefik_admin',
  };

  it('renders permission empty state when accessed by non-admin user', () => {
    const html = renderToString(
      <MemoryRouter>
        <ModuleUserProvider
          value={{
            user: normalUser,
            loadingUser: false,
            isEmbedded: false,
            reloadUser: async () => {},
          }}
        >
          <AdminSettingsPage />
        </ModuleUserProvider>
      </MemoryRouter>
    );

    expect(html).toContain('需要模块管理员权限');
    expect(html).not.toContain('CI/CD 自动化门禁凭据');
  });

  it('renders CI/CD gate token card and settings form for admin user', () => {
    const html = renderToString(
      <MemoryRouter>
        <ModuleUserProvider
          value={{
            user: adminUser,
            loadingUser: false,
            isEmbedded: false,
            reloadUser: async () => {},
          }}
        >
          <AdminSettingsPage />
        </ModuleUserProvider>
      </MemoryRouter>
    );

    // 必须包含 CI/CD 自动化门禁凭据治理卡片与安全出向/LLM 治理表单
    expect(html).toContain('CI/CD 自动化门禁凭据');
    expect(html).toContain('POST /api/gates/evaluate');
    expect(html).toContain('生成 CI Token');
    expect(html).toContain('运行时动态配置项');
    expect(html).toContain('outbound_trusted_cidrs');
    expect(html).toContain('llm_enabled');
    expect(html).toContain('llm_base_url');
    expect(html).toContain('llm_api_key');
    expect(html).toContain('code_notifier_token');
    expect(html).toContain('smtp_password');

    // 安全红线：绝不能泄露或渲染明文 Token 键或内部加密/哈希键名
    expect(html).not.toContain('ci_gate_token_sha256');
    expect(html).not.toContain('llm_api_key_enc');
    expect(html).not.toContain('code_notifier_token_enc');
    expect(html).not.toContain('smtp_password_enc');
  });
});
