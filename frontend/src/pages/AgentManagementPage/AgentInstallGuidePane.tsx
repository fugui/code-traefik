import React, { useState } from 'react';

export interface AgentInstallGuidePaneProps {
  onOpenRegisterModal: () => void;
}

export const AgentInstallGuidePane: React.FC<AgentInstallGuidePaneProps> = ({
  onOpenRegisterModal,
}) => {
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  // 解析当前服务器 IP / Host 用于动态建议
  const host = typeof window !== 'undefined' ? window.location.hostname : '192.168.56.18';
  const origin = typeof window !== 'undefined' ? window.location.origin : 'http://192.168.56.18:8000';
  const downloadUrl = `${origin}/traefik/api/agent/download`;

  const curlCommand = `curl -fsSL "${downloadUrl}" -o code-traefik-agent && chmod +x code-traefik-agent`;
  const doctorCommand = `sudo ./code-traefik-agent --doctor`;
  const startCommand = `sudo ./code-traefik-agent --listen 0.0.0.0:9091 --allow-ips "${host},127.0.0.1"`;

  const handleCopy = (text: string, key: string) => {
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      void navigator.clipboard.writeText(text);
      setCopiedKey(key);
      setTimeout(() => {
        setCopiedKey((curr) => (curr === key ? null : curr));
      }, 2000);
    }
  };

  return (
    <div className="code-agent-install-guide" data-testid="agent-install-guide-pane">
      {/* 顶部指引总览 */}
      <div className="code-guide-header">
        <div className="code-guide-header__badge">
          <span className="code-badge code-badge--info">自服务部署指南</span>
          <span className="code-badge code-badge--secondary">单二进制 · 零依赖待命</span>
        </div>
        <h3 className="code-guide-header__title">code-traefik-agent 部署与快速接入</h3>
        <p className="code-guide-header__desc">
          <code>code-traefik-agent</code> 是部署在业务宿主机或测试虚机上的无状态常驻探针。基于 Linux 内核 eBPF 技术，实现免重启的进程流量捕获、精准套接字追踪与排他录制重放。
          只需完成以下 4 个步骤，即可将您的私有开发机或测试环境纳管至平台。
        </p>
      </div>

      <div className="code-guide-steps-grid">
        {/* 步骤 1: 获取二进制 */}
        <div className="code-guide-step-card" data-testid="guide-step-download">
          <div className="code-guide-step-card__header">
            <span className="code-step-number">1</span>
            <div className="code-step-title-group">
              <h4 className="code-step-title">获取 Agent 单二进制可执行文件</h4>
              <span className="code-step-subtitle">支持浏览器直接下载或在 Linux 终端一键拉取</span>
            </div>
          </div>

          <div className="code-guide-step-card__body">
            <div className="code-download-actions">
              <a
                href={downloadUrl}
                download="code-traefik-agent"
                className="code-btn code-btn--primary"
                data-testid="direct-download-btn"
              >
                📥 浏览器直接下载 (Linux amd64)
              </a>
              <span className="code-text-muted code-text-xs">单二进制约 12MB，无外部运行依赖</span>
            </div>

            <div className="code-cmd-block code-mt-sm">
              <div className="code-cmd-block__header">
                <span className="code-cmd-block__label">在目标机器上通过 curl 快速拉取并赋予执行权限：</span>
                <button
                  type="button"
                  className="code-copy-btn"
                  data-testid="copy-curl-btn"
                  onClick={() => handleCopy(curlCommand, 'curl')}
                >
                  {copiedKey === 'curl' ? '✓ 已复制' : '复制命令'}
                </button>
              </div>
              <pre className="code-cmd-code code-font-mono" data-testid="curl-command-text">
                {curlCommand}
              </pre>
            </div>
          </div>
        </div>

        {/* 步骤 2: 环境就绪体检 */}
        <div className="code-guide-step-card" data-testid="guide-step-doctor">
          <div className="code-guide-step-card__header">
            <span className="code-step-number">2</span>
            <div className="code-step-title-group">
              <h4 className="code-step-title">环境与内核特权就绪诊断 (Doctor)</h4>
              <span className="code-step-subtitle">验证 Linux 内核版本 (&ge; 5.8) 及 eBPF 探针特权完整性</span>
            </div>
          </div>

          <div className="code-guide-step-card__body">
            <p className="code-step-p">
              在首次启动前，建议运行 <code>--doctor</code> 诊断模式。Agent 将自动体检内核支持情况、Capabilities 权限与 <code>/sys/fs/bpf</code> 挂载状态：
            </p>

            <div className="code-cmd-block">
              <div className="code-cmd-block__header">
                <span className="code-cmd-block__label">执行环境特权就绪体检命令：</span>
                <button
                  type="button"
                  className="code-copy-btn"
                  data-testid="copy-doctor-btn"
                  onClick={() => handleCopy(doctorCommand, 'doctor')}
                >
                  {copiedKey === 'doctor' ? '✓ 已复制' : '复制命令'}
                </button>
              </div>
              <pre className="code-cmd-code code-font-mono" data-testid="doctor-command-text">
                {doctorCommand}
              </pre>
            </div>

            <div className="code-guide-doctor-items code-mt-sm">
              <div className="code-doctor-pill">
                <span className="code-status-dot code-status-dot--online" />
                <span>[1/4] Linux 操作系统支持</span>
              </div>
              <div className="code-doctor-pill">
                <span className="code-status-dot code-status-dot--online" />
                <span>[2/4] 内核版本 &ge; 5.8 (BPF RingBuf)</span>
              </div>
              <div className="code-doctor-pill">
                <span className="code-status-dot code-status-dot--online" />
                <span>[3/4] CAP_BPF / sudo 权限完整</span>
              </div>
              <div className="code-doctor-pill">
                <span className="code-status-dot code-status-dot--online" />
                <span>[4/4] /proc 与 /sys/fs/bpf 虚拟挂载</span>
              </div>
            </div>
          </div>
        </div>

        {/* 步骤 3: 启动 Agent 并提取认领码 */}
        <div className="code-guide-step-card" data-testid="guide-step-start">
          <div className="code-guide-step-card__header">
            <span className="code-step-number">3</span>
            <div className="code-step-title-group">
              <h4 className="code-step-title">启动 Agent 守护进程并配置白名单</h4>
              <span className="code-step-subtitle">配置管控服务器 IP 白名单与监听端点</span>
            </div>
          </div>

          <div className="code-guide-step-card__body">
            <div className="code-cmd-block">
              <div className="code-cmd-block__header">
                <span className="code-cmd-block__label">以特权身份常驻启动 Agent：</span>
                <button
                  type="button"
                  className="code-copy-btn"
                  data-testid="copy-start-btn"
                  onClick={() => handleCopy(startCommand, 'start')}
                >
                  {copiedKey === 'start' ? '✓ 已复制' : '复制命令'}
                </button>
              </div>
              <pre className="code-cmd-code code-font-mono" data-testid="start-command-text">
                {startCommand}
              </pre>
            </div>

            <div className="code-param-table-wrapper code-mt-sm">
              <table className="code-table code-table--compact">
                <thead>
                  <tr>
                    <th>参数标志</th>
                    <th>说明</th>
                    <th>推荐值</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td><code>--listen</code></td>
                    <td>Agent 绑定的监听地址与端口</td>
                    <td><code>0.0.0.0:9091</code></td>
                  </tr>
                  <tr>
                    <td><code>--allow-ips</code></td>
                    <td>受信任主控 IP 白名单（逗号分隔）</td>
                    <td>当前管控端 IP: <code>{host}</code></td>
                  </tr>
                  <tr>
                    <td><code>--claim-code</code></td>
                    <td>预设私有认领码（留空将自动在控制台打印一次性认领凭证）</td>
                    <td>选填</td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div className="code-log-preview-box code-mt-sm">
              <span className="code-log-preview-box__title">💡 终端启动日志示例（请关注认领码 ClaimCode）：</span>
              <pre className="code-log-preview-box__content code-font-mono">
{`[Agent] 启动 code-traefik-agent 版本: v0.1.0 (GitCommit: dev, BuildTime: 2026-11-07)
[Agent] 监听地址: 0.0.0.0:9091, 白名单: [${host} 127.0.0.1]
[Agent] 认领码 (ClaimCode): c7a8b9f2e1
[Agent] 实例 BootID: e702ba7c-728b-4a55-a226-c116c49ce3ea
[Agent] [✓] 内核特权检测通过: CAP_BPF, CAP_NET_ADMIN, CAP_SYS_ADMIN 均就绪`}
              </pre>
            </div>
          </div>
        </div>

        {/* 步骤 4: 立即纳管登记 */}
        <div className="code-guide-step-card code-guide-step-card--cta" data-testid="guide-step-register">
          <div className="code-guide-step-card__header">
            <span className="code-step-number">4</span>
            <div className="code-step-title-group">
              <h4 className="code-step-title">完成纳管登记并开始调试</h4>
              <span className="code-step-subtitle">在控制台登记您的私有 Agent，即可独占挂载业务进程并录制流量</span>
            </div>
          </div>

          <div className="code-guide-step-card__body">
            <p className="code-step-p">
              当目标主机上的 Agent 显示 <code>[Agent] 认领码 (ClaimCode)</code> 后，点击下方按钮打开登记弹窗。输入目标主机的 IP 端点与认领码即可完成安全绑定。
            </p>

            <div className="code-cta-action-area code-mt-md">
              <button
                type="button"
                className="code-btn code-btn--primary code-btn--lg"
                data-testid="cta-register-agent-btn"
                onClick={onOpenRegisterModal}
              >
                🚀 已在主机启动完成？立即登记私有 Agent
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
