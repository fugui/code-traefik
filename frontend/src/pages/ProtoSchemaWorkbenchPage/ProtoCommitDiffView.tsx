import React, { useMemo } from 'react';
import { GitCompare } from 'lucide-react';
import './ProtoDocsAndCodeViewer.css';

export interface ProtoCommitDiffViewProps {
  filePath: string;
  fromVersionLabel: string;
  toVersionLabel: string;
  unifiedDiff: string;
  addedLines: number;
  deletedLines: number;
  commitSummary?: string;
  authorName?: string;
}

interface ParsedDiffLine {
  kind: 'hunk' | 'add' | 'del' | 'context';
  sign: string;
  text: string;
}

function parseUnifiedDiffLines(unifiedDiff: string): ParsedDiffLine[] {
  const normalized = unifiedDiff.replace(/\r\n/g, '\n');
  const rawLines = normalized.split('\n');
  if (rawLines.length > 1 && rawLines[rawLines.length - 1] === '') {
    rawLines.pop();
  }

  return rawLines.map((line) => {
    if (line.startsWith('---') || line.startsWith('+++') || line.startsWith('@@')) {
      return { kind: 'hunk', sign: '@@', text: line };
    }
    if (line.startsWith('+')) {
      return { kind: 'add', sign: '+', text: line.slice(1) };
    }
    if (line.startsWith('-')) {
      return { kind: 'del', sign: '-', text: line.slice(1) };
    }
    return {
      kind: 'context',
      sign: ' ',
      text: line.startsWith(' ') ? line.slice(1) : line,
    };
  });
}

export const ProtoCommitDiffView: React.FC<ProtoCommitDiffViewProps> = ({
  filePath,
  fromVersionLabel,
  toVersionLabel,
  unifiedDiff,
  addedLines,
  deletedLines,
  commitSummary,
  authorName,
}) => {
  const parsedLines = useMemo(() => parseUnifiedDiffLines(unifiedDiff), [unifiedDiff]);

  return (
    <div className="pdoc-diff-viewer" data-testid="proto-commit-diff-view">
      <div className="pdoc-diff-viewer__header">
        <div className="pdoc-meta-row">
          <GitCompare size={15} />
          <span className="pdoc-code-viewer__filepath">{filePath}</span>
          <span className="pdoc-pill">{fromVersionLabel}</span>
          <span>→</span>
          <span className="pdoc-pill">{toVersionLabel}</span>
        </div>
        <div className="pdoc-meta-row">
          {authorName && <span className="pdoc-pill">提交人: {authorName}</span>}
          {commitSummary && <span className="pdoc-pill">{commitSummary}</span>}
          <span className="pdoc-diff-stat--add">+{addedLines}</span>
          <span className="pdoc-diff-stat--del">-{deletedLines}</span>
        </div>
      </div>

      <div className="pdoc-code-scroll">
        <table className="pdoc-diff-table" role="presentation">
          <tbody>
            {parsedLines.map((row, idx) => {
              const rowClass =
                row.kind === 'add'
                  ? 'pdoc-diff-row--add'
                  : row.kind === 'del'
                    ? 'pdoc-diff-row--del'
                    : row.kind === 'hunk'
                      ? 'pdoc-diff-row--hunk'
                      : '';
              return (
                <tr key={idx} className={rowClass}>
                  <td className="pdoc-diff-cell__sign">{row.sign}</td>
                  <td className="pdoc-diff-cell__text">{row.text}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
};
