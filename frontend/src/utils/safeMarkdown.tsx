import React from 'react';

interface SafeMarkdownTextProps {
  content: string;
  className?: string;
}

/**
 * 极简只读安全文本与受限 Markdown 片段渲染器：
 * 仅解析标题（#/##/###）、无序列表（- / *）、行内代码（`code`）与加粗（**text**），
 * 全程生成 React 原生 JSX 节点，严禁注入原始 HTML 与引入重型三方依赖造成打包体积膨胀。
 */
function renderInlineSegments(line: string, keyPrefix: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  const tokenRegex = /(`[^`]+`|\*\*[^*]+\*\*)/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null = tokenRegex.exec(line);
  let partIdx = 0;

  while (match !== null) {
    if (match.index > lastIndex) {
      nodes.push(line.slice(lastIndex, match.index));
    }
    const token = match[0];
    if (token.startsWith('`') && token.endsWith('`')) {
      nodes.push(
        <code key={`${keyPrefix}-code-${partIdx}`} className="code-tpl-code-inline">
          {token.slice(1, -1)}
        </code>
      );
    } else if (token.startsWith('**') && token.endsWith('**')) {
      nodes.push(
        <strong key={`${keyPrefix}-bold-${partIdx}`}>{token.slice(2, -2)}</strong>
      );
    }
    lastIndex = tokenRegex.lastIndex;
    partIdx += 1;
    match = tokenRegex.exec(line);
  }

  if (lastIndex < line.length) {
    nodes.push(line.slice(lastIndex));
  }
  return nodes;
}

export const SafeMarkdownText: React.FC<SafeMarkdownTextProps> = ({ content, className }) => {
  const normalized = (content || '').replace(/\r\n/g, '\n').trim();
  if (!normalized) {
    return null;
  }

  const lines = normalized.split('\n');
  const blocks: React.ReactNode[] = [];
  let listItems: string[] = [];

  const flushList = (keySuffix: number) => {
    if (listItems.length === 0) return;
    blocks.push(
      <ul key={`ul-${keySuffix}`} className="safe-md-list">
        {listItems.map((item, idx) => (
          <li key={`li-${keySuffix}-${idx}`}>{renderInlineSegments(item, `li-${keySuffix}-${idx}`)}</li>
        ))}
      </ul>
    );
    listItems = [];
  };

  lines.forEach((rawLine, idx) => {
    const trimmed = rawLine.trim();
    if (!trimmed) {
      flushList(idx);
      return;
    }

    if (trimmed.startsWith('- ') || trimmed.startsWith('* ')) {
      listItems.push(trimmed.slice(2).trim());
      return;
    }

    flushList(idx);

    if (trimmed.startsWith('### ')) {
      blocks.push(
        <h5 key={`h5-${idx}`} className="safe-md-heading">
          {renderInlineSegments(trimmed.slice(4), `h5-${idx}`)}
        </h5>
      );
      return;
    }
    if (trimmed.startsWith('## ')) {
      blocks.push(
        <h4 key={`h4-${idx}`} className="safe-md-heading">
          {renderInlineSegments(trimmed.slice(3), `h4-${idx}`)}
        </h4>
      );
      return;
    }
    if (trimmed.startsWith('# ')) {
      blocks.push(
        <h4 key={`h3-${idx}`} className="safe-md-heading">
          {renderInlineSegments(trimmed.slice(2), `h3-${idx}`)}
        </h4>
      );
      return;
    }

    blocks.push(
      <p key={`p-${idx}`} className="safe-md-paragraph">
        {renderInlineSegments(trimmed, `p-${idx}`)}
      </p>
    );
  });

  flushList(lines.length);

  return <div className={className ? `safe-md-root ${className}` : 'safe-md-root'}>{blocks}</div>;
};

export default SafeMarkdownText;
