import React, { useEffect, useState } from 'react';
import { FolderGit2, KeyRound, Layers, Plus, RefreshCw, Trash2 } from 'lucide-react';
import { Drawer } from '@code/common';
import * as schemaApi from '../../api/schema';
import { useToast } from '../../utils/toast';
import type {
  GitAuthType,
  GitProtoBundleDTO,
  GitProviderType,
  GitRepoBundleInput,
  GitRepoDTO,
  GitSyncMode,
  MRAutoPublishMode,
  ProtoCategory,
} from '../../types';
import './ProtoDocsAndCodeViewer.css';

export interface GitRepoConfigDrawerProps {
  open: boolean;
  isAdmin: boolean;
  repo?: GitRepoDTO | null;
  onClose: () => void;
  onSaved: (repo: GitRepoDTO, created: boolean) => void;
  onDeleted?: (repoId: number) => void;
}

interface RepoFormState {
  name: string;
  provider: GitProviderType;
  repo_url: string;
  api_base_url: string;
  default_branch: string;
  auth_type: GitAuthType;
  credential_plain: string;
  webhook_secret_plain: string;
  sync_mode: GitSyncMode;
  poll_interval_sec: number;
  auto_compile: boolean;
  auto_activate: boolean;
  lint_on_sync: boolean;
  description: string;
}

interface BundleFormState {
  bundle_name: string;
  target_schema_name: string;
  category: ProtoCategory;
  subdir_path: string;
  include_dirs_text: string;
  entry_files_text: string;
  file_globs_text: string;
  exclude_globs_text: string;
  auto_compile: boolean;
  auto_activate: boolean;
  mr_auto_publish: MRAutoPublishMode;
  lint_on_sync: boolean;
  description: string;
}

function splitCommaList(raw: string): string[] {
  return raw
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}

function createInitialRepoForm(repo?: GitRepoDTO | null): RepoFormState {
  return {
    name: repo?.name || '',
    provider: (repo?.provider as GitProviderType) || 'GITLAB',
    repo_url: repo?.repo_url || '',
    api_base_url: repo?.api_base_url || '',
    default_branch: repo?.default_branch || 'main',
    auth_type: (repo?.auth_type as GitAuthType) || 'TOKEN',
    credential_plain: '',
    webhook_secret_plain: '',
    sync_mode: (repo?.sync_mode as GitSyncMode) || 'MANUAL',
    poll_interval_sec: repo?.poll_interval_sec ?? 300,
    auto_compile: repo?.auto_compile ?? true,
    auto_activate: repo?.auto_activate ?? false,
    lint_on_sync: repo?.lint_on_sync ?? true,
    description: repo?.description || '',
  };
}

function createInitialBundleForm(bundle?: GitProtoBundleDTO | null): BundleFormState {
  return {
    bundle_name: bundle?.bundle_name || 'default',
    target_schema_name: bundle?.target_schema_name || '',
    category: (bundle?.category as ProtoCategory) || 'BUSINESS',
    subdir_path: bundle?.subdir_path || '',
    include_dirs_text: bundle?.include_dirs?.join(', ') || '.',
    entry_files_text: bundle?.entry_files?.join(', ') || '',
    file_globs_text: bundle?.file_globs?.join(', ') || '*.proto',
    exclude_globs_text: bundle?.exclude_globs?.join(', ') || '',
    auto_compile: bundle?.auto_compile ?? true,
    auto_activate: bundle?.auto_activate ?? false,
    mr_auto_publish: (bundle?.mr_auto_publish as MRAutoPublishMode) || 'OFF',
    lint_on_sync: bundle?.lint_on_sync ?? true,
    description: bundle?.description || '',
  };
}

function toBundlePayload(form: BundleFormState): GitRepoBundleInput {
  return {
    bundle_name: form.bundle_name.trim() || 'default',
    target_schema_name: form.target_schema_name.trim(),
    category: form.category,
    subdir_path: form.subdir_path.trim(),
    include_dirs: splitCommaList(form.include_dirs_text),
    entry_files: splitCommaList(form.entry_files_text),
    file_globs: splitCommaList(form.file_globs_text),
    exclude_globs: splitCommaList(form.exclude_globs_text),
    auto_compile: form.auto_compile,
    auto_activate: form.auto_activate,
    mr_auto_publish: form.mr_auto_publish,
    lint_on_sync: form.lint_on_sync,
    description: form.description.trim(),
  };
}

export const GitRepoConfigDrawer: React.FC<GitRepoConfigDrawerProps> = ({
  open,
  isAdmin,
  repo,
  onClose,
  onSaved,
  onDeleted,
}) => {
  const toast = useToast();
  const isEdit = Boolean(repo && repo.id > 0);

  const [repoForm, setRepoForm] = useState<RepoFormState>(() => createInitialRepoForm(repo));
  const [defaultBundleForm, setDefaultBundleForm] = useState<BundleFormState>(() =>
    createInitialBundleForm(null)
  );
  const [submitting, setSubmitting] = useState(false);
  const [confirmDeleteRepo, setConfirmDeleteRepo] = useState(false);

  // 编辑模式下的 1:N Bundle 子表单状态
  const [bundleEditorOpen, setBundleEditorOpen] = useState(false);
  const [editingBundle, setEditingBundle] = useState<GitProtoBundleDTO | null>(null);
  const [bundleForm, setBundleForm] = useState<BundleFormState>(() => createInitialBundleForm(null));
  const [bundleSaving, setBundleSaving] = useState(false);
  const [confirmDeleteBundleId, setConfirmDeleteBundleId] = useState<number | null>(null);

  useEffect(() => {
    setRepoForm(createInitialRepoForm(repo));
    setConfirmDeleteRepo(false);
    setBundleEditorOpen(false);
    setEditingBundle(null);
    setConfirmDeleteBundleId(null);
    if (!repo) {
      setDefaultBundleForm(createInitialBundleForm(null));
    }
  }, [repo, open]);

  const handleSaveRepo = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isAdmin) return;
    if (!repoForm.name.trim() || !repoForm.repo_url.trim()) {
      toast.warning('请填写仓库名称与 Git 克隆地址');
      return;
    }

    setSubmitting(true);
    try {
      if (isEdit && repo) {
        const updated = await schemaApi.updateGitRepo(repo.id, {
          name: repoForm.name.trim(),
          provider: repoForm.provider,
          repo_url: repoForm.repo_url.trim(),
          api_base_url: repoForm.api_base_url.trim(),
          default_branch: repoForm.default_branch.trim() || 'main',
          auth_type: repoForm.auth_type,
          credential_plain: repoForm.credential_plain.trim() || undefined,
          webhook_secret_plain: repoForm.webhook_secret_plain.trim() || undefined,
          sync_mode: repoForm.sync_mode,
          poll_interval_sec: Number(repoForm.poll_interval_sec) || 300,
          auto_compile: repoForm.auto_compile,
          auto_activate: repoForm.auto_activate,
          lint_on_sync: repoForm.lint_on_sync,
          description: repoForm.description.trim(),
        });
        toast.success(`Git 仓库 ${updated.name} 配置已更新`);
        onSaved(updated, false);
      } else {
        const schemaName = defaultBundleForm.target_schema_name.trim() || repoForm.name.trim();
        const created = await schemaApi.createGitRepo({
          name: repoForm.name.trim(),
          provider: repoForm.provider,
          repo_url: repoForm.repo_url.trim(),
          api_base_url: repoForm.api_base_url.trim(),
          default_branch: repoForm.default_branch.trim() || 'main',
          auth_type: repoForm.auth_type,
          credential_plain: repoForm.credential_plain.trim() || undefined,
          webhook_secret_plain: repoForm.webhook_secret_plain.trim() || undefined,
          sync_mode: repoForm.sync_mode,
          poll_interval_sec: Number(repoForm.poll_interval_sec) || 300,
          auto_compile: repoForm.auto_compile,
          auto_activate: repoForm.auto_activate,
          lint_on_sync: repoForm.lint_on_sync,
          description: repoForm.description.trim(),
          default_bundle: {
            ...toBundlePayload(defaultBundleForm),
            target_schema_name: schemaName,
          },
        });
        toast.success(`Git 仓库 ${created.name} 已接入并完成脱敏加密存储`);
        onSaved(created, true);
      }
    } catch (err) {
      toast.error((err as Error).message || '保存 Git 仓库失败');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDeleteRepo = async () => {
    if (!isAdmin || !repo) return;
    setSubmitting(true);
    try {
      await schemaApi.deleteGitRepo(repo.id);
      toast.success(`已删除仓库 ${repo.name}`);
      onDeleted?.(repo.id);
      onClose();
    } catch (err) {
      toast.error((err as Error).message || '删除仓库失败');
    } finally {
      setSubmitting(false);
    }
  };

  const openBundleEditor = (bundle?: GitProtoBundleDTO) => {
    setEditingBundle(bundle ?? null);
    setBundleForm(
      bundle
        ? createInitialBundleForm(bundle)
        : createInitialBundleForm(null)
    );
    setBundleEditorOpen(true);
  };

  const handleSaveBundle = async () => {
    if (!isAdmin || !repo) return;
    if (!bundleForm.bundle_name.trim() || !bundleForm.target_schema_name.trim()) {
      toast.warning('请填写 Bundle 名称与目标 Schema 资产名称');
      return;
    }

    setBundleSaving(true);
    try {
      const payload = toBundlePayload(bundleForm);
      if (editingBundle) {
        await schemaApi.updateGitRepoBundle(repo.id, editingBundle.id, payload);
        toast.success(`Bundle [${payload.bundle_name}] 已更新`);
      } else {
        await schemaApi.createGitRepoBundle(repo.id, payload);
        toast.success(`Monorepo Bundle [${payload.bundle_name}] 已创建`);
      }
      const refreshed = await schemaApi.getGitRepoDetail(repo.id);
      setBundleEditorOpen(false);
      setEditingBundle(null);
      onSaved(refreshed, false);
    } catch (err) {
      toast.error((err as Error).message || '保存 Bundle 失败');
    } finally {
      setBundleSaving(false);
    }
  };

  const handleDeleteBundle = async (bundleId: number) => {
    if (!isAdmin || !repo) return;
    setBundleSaving(true);
    try {
      await schemaApi.deleteGitRepoBundle(repo.id, bundleId);
      const refreshed = await schemaApi.getGitRepoDetail(repo.id);
      setConfirmDeleteBundleId(null);
      toast.success('Bundle 映射已移除');
      onSaved(refreshed, false);
    } catch (err) {
      toast.error((err as Error).message || '删除 Bundle 失败');
    } finally {
      setBundleSaving(false);
    }
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={isEdit && repo ? `Git 仓库与 Monorepo 包配置：${repo.name}` : '接入新 Git Proto 仓库'}
      subtitle="凭据采用 AES-256-GCM 服务端加密存储，接口响应全程严格脱敏"
      width={780}
    >
      <div className="pgit-drawer-stack">
        <form onSubmit={(e) => void handleSaveRepo(e)} className="pdoc-card">
          <div className="pdoc-card__header">
            <h4 className="pdoc-card__title">
              <FolderGit2 size={16} /> 仓库连接与安全认证配置
            </h4>
            {isEdit && repo && (
              <div className="pdoc-meta-row">
                {repo.has_credentials && repo.credential_hint && (
                  <span className="pdoc-pill" title="脱敏凭据指纹">
                    <KeyRound size={12} /> 凭据指纹: <code>{repo.credential_hint}</code>
                  </span>
                )}
                {repo.has_webhook_secret && (
                  <span className="code-badge code-badge-success">Webhook Secret 已配置</span>
                )}
              </div>
            )}
          </div>

          <div className="pgit-form-grid">
            <label className="pgit-field">
              <span className="pgit-field__label">仓库标识名 *</span>
              <input
                type="text"
                className="pgit-input"
                value={repoForm.name}
                disabled={!isAdmin}
                onChange={(e) => setRepoForm((prev) => ({ ...prev, name: e.target.value }))}
                placeholder="例如 dophi-monorepo"
              />
            </label>

            <label className="pgit-field">
              <span className="pgit-field__label">Git 托管平台 *</span>
              <select
                className="pgit-input"
                value={repoForm.provider}
                disabled={!isAdmin}
                onChange={(e) =>
                  setRepoForm((prev) => ({
                    ...prev,
                    provider: e.target.value as GitProviderType,
                  }))
                }
              >
                <option value="GITLAB">GitLab / 自建 GitLab</option>
                <option value="GITHUB">GitHub / GitHub Enterprise</option>
                <option value="GITEA">Gitea / Forgejo</option>
                <option value="CUSTOM_GIT">通用 Git（go-git MemFS 无状态拉取）</option>
              </select>
            </label>

            <label className="pgit-field pgit-field--wide">
              <span className="pgit-field__label">仓库 HTTPS / SSH URL *</span>
              <input
                type="text"
                className="pgit-input"
                value={repoForm.repo_url}
                disabled={!isAdmin}
                onChange={(e) => setRepoForm((prev) => ({ ...prev, repo_url: e.target.value }))}
                placeholder="https://git.example.com/group/contracts.git"
              />
            </label>

            <label className="pgit-field">
              <span className="pgit-field__label">REST API Base URL（自建站点必填）</span>
              <input
                type="text"
                className="pgit-input"
                value={repoForm.api_base_url}
                disabled={!isAdmin}
                onChange={(e) =>
                  setRepoForm((prev) => ({ ...prev, api_base_url: e.target.value }))
                }
                placeholder="https://git.example.com"
              />
            </label>

            <label className="pgit-field">
              <span className="pgit-field__label">默认基线分支</span>
              <input
                type="text"
                className="pgit-input"
                value={repoForm.default_branch}
                disabled={!isAdmin}
                onChange={(e) =>
                  setRepoForm((prev) => ({ ...prev, default_branch: e.target.value }))
                }
                placeholder="main"
              />
            </label>

            <label className="pgit-field">
              <span className="pgit-field__label">认证方式</span>
              <select
                className="pgit-input"
                value={repoForm.auth_type}
                disabled={!isAdmin}
                onChange={(e) =>
                  setRepoForm((prev) => ({ ...prev, auth_type: e.target.value as GitAuthType }))
                }
              >
                <option value="NONE">公开匿名仓库 (NONE)</option>
                <option value="TOKEN">Access Token / PAT (TOKEN)</option>
                <option value="BASIC">账号密码 Basic (username:password)</option>
                <option value="SSH_KEY">SSH 私钥 (SSH_KEY)</option>
              </select>
            </label>

            <label className="pgit-field">
              <span className="pgit-field__label">同步触发策略</span>
              <select
                className="pgit-input"
                value={repoForm.sync_mode}
                disabled={!isAdmin}
                onChange={(e) =>
                  setRepoForm((prev) => ({ ...prev, sync_mode: e.target.value as GitSyncMode }))
                }
              >
                <option value="WEBHOOK">Webhook 事件驱动</option>
                <option value="POLLING">后台定时轮询 (Polling)</option>
                <option value="MANUAL">仅手动同步 (Manual)</option>
              </select>
            </label>

            {repoForm.auth_type !== 'NONE' && (
              <label className="pgit-field pgit-field--wide">
                <span className="pgit-field__label">
                  访问凭据（Token / Basic / 私钥，提交后密文落库）
                </span>
                <input
                  type="password"
                  className="pgit-input"
                  value={repoForm.credential_plain}
                  disabled={!isAdmin}
                  onChange={(e) =>
                    setRepoForm((prev) => ({ ...prev, credential_plain: e.target.value }))
                  }
                  placeholder={
                    isEdit && repo?.has_credentials
                      ? `已保存加密凭据 (${repo.credential_hint || '已脱敏'})；留空则保持原凭据不变`
                      : '输入 Git Token 或 Basic 凭据'
                  }
                />
              </label>
            )}

            <label className="pgit-field">
              <span className="pgit-field__label">Webhook Secret（留空不变）</span>
              <input
                type="password"
                className="pgit-input"
                value={repoForm.webhook_secret_plain}
                disabled={!isAdmin}
                onChange={(e) =>
                  setRepoForm((prev) => ({ ...prev, webhook_secret_plain: e.target.value }))
                }
                placeholder={
                  isEdit && repo?.has_webhook_secret ? '已配置密文 Secret，留空保持不变' : '可选'
                }
              />
            </label>

            <label className="pgit-field">
              <span className="pgit-field__label">仓库描述备注</span>
              <input
                type="text"
                className="pgit-input"
                value={repoForm.description}
                disabled={!isAdmin}
                onChange={(e) =>
                  setRepoForm((prev) => ({ ...prev, description: e.target.value }))
                }
                placeholder="团队 / 领域边界说明"
              />
            </label>
          </div>

          <div className="pgit-checkbox-row">
            <label className="pgit-check">
              <input
                type="checkbox"
                checked={repoForm.auto_compile}
                disabled={!isAdmin}
                onChange={(e) =>
                  setRepoForm((prev) => ({ ...prev, auto_compile: e.target.checked }))
                }
              />
              同步后纯内存自动编译描述符 (protocompile)
            </label>
            <label className="pgit-check">
              <input
                type="checkbox"
                checked={repoForm.auto_activate}
                disabled={!isAdmin}
                onChange={(e) =>
                  setRepoForm((prev) => ({ ...prev, auto_activate: e.target.checked }))
                }
              />
              自动设为活跃版本
            </label>
            <label className="pgit-check">
              <input
                type="checkbox"
                checked={repoForm.lint_on_sync}
                disabled={!isAdmin}
                onChange={(e) =>
                  setRepoForm((prev) => ({ ...prev, lint_on_sync: e.target.checked }))
                }
              />
              同步时自动触发 Lint 静态规范检查
            </label>
          </div>

          {!isEdit && (
            <div className="pgit-subpanel">
              <h5 className="pdoc-section-title">初始默认 Bundle 映射（可在创建后继续追加 Monorepo 多包）</h5>
              <div className="pgit-form-grid">
                <label className="pgit-field">
                  <span className="pgit-field__label">Bundle 名称</span>
                  <input
                    type="text"
                    className="pgit-input"
                    value={defaultBundleForm.bundle_name}
                    onChange={(e) =>
                      setDefaultBundleForm((prev) => ({ ...prev, bundle_name: e.target.value }))
                    }
                  />
                </label>
                <label className="pgit-field">
                  <span className="pgit-field__label">目标 Schema 资产名称 *</span>
                  <input
                    type="text"
                    className="pgit-input"
                    value={defaultBundleForm.target_schema_name}
                    onChange={(e) =>
                      setDefaultBundleForm((prev) => ({
                        ...prev,
                        target_schema_name: e.target.value,
                      }))
                    }
                    placeholder="默认同仓库名，如 order-proto"
                  />
                </label>
                <label className="pgit-field">
                  <span className="pgit-field__label">资产分层分类</span>
                  <select
                    className="pgit-input"
                    value={defaultBundleForm.category}
                    onChange={(e) =>
                      setDefaultBundleForm((prev) => ({
                        ...prev,
                        category: e.target.value as ProtoCategory,
                      }))
                    }
                  >
                    <option value="BASE_LIB">BASE_LIB（基础模型库）</option>
                    <option value="BUSINESS">BUSINESS（业务服务）</option>
                    <option value="GATEWAY">GATEWAY（网关聚合）</option>
                  </select>
                </label>
                <label className="pgit-field">
                  <span className="pgit-field__label">Monorepo 子目录 (subdir_path)</span>
                  <input
                    type="text"
                    className="pgit-input"
                    value={defaultBundleForm.subdir_path}
                    onChange={(e) =>
                      setDefaultBundleForm((prev) => ({ ...prev, subdir_path: e.target.value }))
                    }
                    placeholder="留空为仓根，或填 services/order"
                  />
                </label>
              </div>
            </div>
          )}

          {isAdmin && (
            <div className="pgit-drawer-actions">
              <div>
                {isEdit && repo && (
                  confirmDeleteRepo ? (
                    <div className="pdoc-meta-row">
                      <button
                        type="button"
                        className="btn btn-danger"
                        disabled={submitting}
                        onClick={() => void handleDeleteRepo()}
                      >
                        确认移除仓库（保留历史已编译资产）
                      </button>
                      <button
                        type="button"
                        className="btn btn-secondary"
                        onClick={() => setConfirmDeleteRepo(false)}
                      >
                        取消
                      </button>
                    </div>
                  ) : (
                    <button
                      type="button"
                      className="btn btn-secondary"
                      onClick={() => setConfirmDeleteRepo(true)}
                    >
                      <Trash2 size={14} /> 移除仓库
                    </button>
                  )
                )}
              </div>

              <div className="pdoc-meta-row">
                <button type="button" className="btn btn-secondary" onClick={onClose}>
                  关闭
                </button>
                <button type="submit" className="btn btn-primary" disabled={submitting}>
                  {submitting ? <RefreshCw size={14} className="pgit-spin" /> : null}
                  {isEdit ? '保存仓库配置' : '创建并接入仓库'}
                </button>
              </div>
            </div>
          )}
        </form>

        {/* Monorepo 1:N Bundles 管理器 */}
        {isEdit && repo && (
          <section className="pdoc-card">
            <div className="pdoc-card__header">
              <div>
                <h4 className="pdoc-card__title">
                  <Layers size={16} /> Monorepo 多包映射 (Bundles: {repo.bundles?.length || 0})
                </h4>
                <p className="pdoc-card__subtitle">
                  单 Git 仓库可划分多个互不干扰的 Proto 编译单元，跨目录 Import 通过相对路径及 Tier-2 DBProvider 自动解析。
                </p>
              </div>
              {isAdmin && (
                <button
                  type="button"
                  className="btn btn-primary"
                  onClick={() => openBundleEditor()}
                >
                  <Plus size={14} /> 新增 Monorepo 包
                </button>
              )}
            </div>

            {bundleEditorOpen && isAdmin && (
              <div className="pgit-subpanel">
                <h5 className="pdoc-section-title">
                  {editingBundle ? `编辑 Bundle：${editingBundle.bundle_name}` : '新增 Bundle 映射'}
                </h5>
                <div className="pgit-form-grid">
                  <label className="pgit-field">
                    <span className="pgit-field__label">Bundle 标识名 *</span>
                    <input
                      type="text"
                      className="pgit-input"
                      value={bundleForm.bundle_name}
                      onChange={(e) =>
                        setBundleForm((prev) => ({ ...prev, bundle_name: e.target.value }))
                      }
                    />
                  </label>
                  <label className="pgit-field">
                    <span className="pgit-field__label">目标 Schema 资产名 *</span>
                    <input
                      type="text"
                      className="pgit-input"
                      value={bundleForm.target_schema_name}
                      onChange={(e) =>
                        setBundleForm((prev) => ({
                          ...prev,
                          target_schema_name: e.target.value,
                        }))
                      }
                    />
                  </label>
                  <label className="pgit-field">
                    <span className="pgit-field__label">资产分类</span>
                    <select
                      className="pgit-input"
                      value={bundleForm.category}
                      onChange={(e) =>
                        setBundleForm((prev) => ({
                          ...prev,
                          category: e.target.value as ProtoCategory,
                        }))
                      }
                    >
                      <option value="BASE_LIB">BASE_LIB</option>
                      <option value="BUSINESS">BUSINESS</option>
                      <option value="GATEWAY">GATEWAY</option>
                    </select>
                  </label>
                  <label className="pgit-field">
                    <span className="pgit-field__label">子目录 (subdir_path)</span>
                    <input
                      type="text"
                      className="pgit-input"
                      value={bundleForm.subdir_path}
                      onChange={(e) =>
                        setBundleForm((prev) => ({ ...prev, subdir_path: e.target.value }))
                      }
                      placeholder="例如 services/order"
                    />
                  </label>
                  <label className="pgit-field">
                    <span className="pgit-field__label">Include 路径（逗号分隔）</span>
                    <input
                      type="text"
                      className="pgit-input"
                      value={bundleForm.include_dirs_text}
                      onChange={(e) =>
                        setBundleForm((prev) => ({ ...prev, include_dirs_text: e.target.value }))
                      }
                      placeholder="., ../common/proto"
                    />
                  </label>
                  <label className="pgit-field">
                    <span className="pgit-field__label">入口 .proto（逗号分隔，空则扫描子目录）</span>
                    <input
                      type="text"
                      className="pgit-input"
                      value={bundleForm.entry_files_text}
                      onChange={(e) =>
                        setBundleForm((prev) => ({ ...prev, entry_files_text: e.target.value }))
                      }
                    />
                  </label>
                  <label className="pgit-field">
                    <span className="pgit-field__label">包含通配符 (file_globs)</span>
                    <input
                      type="text"
                      className="pgit-input"
                      value={bundleForm.file_globs_text}
                      onChange={(e) =>
                        setBundleForm((prev) => ({ ...prev, file_globs_text: e.target.value }))
                      }
                    />
                  </label>
                  <label className="pgit-field">
                    <span className="pgit-field__label">MR 合入自动发布策略</span>
                    <select
                      className="pgit-input"
                      value={bundleForm.mr_auto_publish}
                      onChange={(e) =>
                        setBundleForm((prev) => ({
                          ...prev,
                          mr_auto_publish: e.target.value as MRAutoPublishMode,
                        }))
                      }
                    >
                      <option value="OFF">关闭 (OFF)</option>
                      <option value="DRAFT_ONLY">仅草稿版本 (DRAFT_ONLY)</option>
                      <option value="AUTO_ACTIVATE">自动激活上线 (AUTO_ACTIVATE)</option>
                    </select>
                  </label>
                </div>

                <div className="pgit-drawer-actions">
                  <div className="pgit-checkbox-row">
                    <label className="pgit-check">
                      <input
                        type="checkbox"
                        checked={bundleForm.auto_compile}
                        onChange={(e) =>
                          setBundleForm((prev) => ({ ...prev, auto_compile: e.target.checked }))
                        }
                      />
                      自动编译
                    </label>
                    <label className="pgit-check">
                      <input
                        type="checkbox"
                        checked={bundleForm.auto_activate}
                        onChange={(e) =>
                          setBundleForm((prev) => ({ ...prev, auto_activate: e.target.checked }))
                        }
                      />
                      自动激活
                    </label>
                  </div>
                  <div className="pdoc-meta-row">
                    <button
                      type="button"
                      className="btn btn-secondary"
                      onClick={() => {
                        setBundleEditorOpen(false);
                        setEditingBundle(null);
                      }}
                    >
                      取消
                    </button>
                    <button
                      type="button"
                      className="btn btn-primary"
                      disabled={bundleSaving}
                      onClick={() => void handleSaveBundle()}
                    >
                      {bundleSaving ? '保存中...' : '保存 Bundle'}
                    </button>
                  </div>
                </div>
              </div>
            )}

            {repo.bundles && repo.bundles.length > 0 ? (
              <table className="code-table">
                <thead>
                  <tr>
                    <th>Bundle 名称</th>
                    <th>目标资产</th>
                    <th>分类</th>
                    <th>子目录 & Include</th>
                    <th>发布策略</th>
                    <th>最近同步</th>
                    {isAdmin && <th>操作</th>}
                  </tr>
                </thead>
                <tbody>
                  {repo.bundles.map((b) => (
                    <tr key={b.id}>
                      <td>
                        <strong>{b.bundle_name}</strong>
                      </td>
                      <td>
                        <code>{b.target_schema_name}</code>
                      </td>
                      <td>
                        <span className="code-badge code-badge-info">{b.category}</span>
                      </td>
                      <td>
                        <div><code>{b.subdir_path || '/'}</code></div>
                        {b.include_dirs?.length > 0 && (
                          <small className="pgit-muted">
                            -I: {b.include_dirs.join(', ')}
                          </small>
                        )}
                      </td>
                      <td>
                        <span className="pdoc-pill">{b.mr_auto_publish || 'OFF'}</span>
                      </td>
                      <td>
                        {b.last_sync_commit ? (
                          <code className="pgit-commit-pill">
                            {b.last_sync_commit.slice(0, 7)}
                          </code>
                        ) : (
                          '—'
                        )}
                      </td>
                      {isAdmin && (
                        <td>
                          <div className="pdoc-meta-row">
                            <button
                              type="button"
                              className="btn btn-secondary"
                              onClick={() => openBundleEditor(b)}
                            >
                              编辑
                            </button>
                            {confirmDeleteBundleId === b.id ? (
                              <button
                                type="button"
                                className="btn btn-danger"
                                disabled={bundleSaving}
                                onClick={() => void handleDeleteBundle(b.id)}
                              >
                                确认删除
                              </button>
                            ) : (
                              <button
                                type="button"
                                className="btn btn-secondary"
                                onClick={() => setConfirmDeleteBundleId(b.id)}
                              >
                                删除
                              </button>
                            )}
                          </div>
                        </td>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className="pdoc-card__subtitle">当前仓库尚未配置任何 Bundle 映射。</p>
            )}
          </section>
        )}
      </div>
    </Drawer>
  );
};

export default GitRepoConfigDrawer;
