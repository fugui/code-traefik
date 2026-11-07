import React from 'react';
import { ArchTopologyTabPane } from '../ArchitectureWorkbenchPage/ArchTopologyTabPane';

export const TopologyWorkbenchPage: React.FC = () => {
  return (
    <div
      className="code-workbench-page code-topology-workbench"
      data-testid="topology-workbench-page"
      style={{
        height: 'calc(100vh - var(--portal-header-height, 64px))',
        width: '100%',
        padding: 0,
        margin: 0,
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        boxSizing: 'border-box',
      }}
    >
      <ArchTopologyTabPane />
    </div>
  );
};

export default TopologyWorkbenchPage;
