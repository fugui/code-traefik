import React from 'react';
import { ArchTopologyTabPane } from '../ArchitectureWorkbenchPage/ArchTopologyTabPane';

export const TopologyWorkbenchPage: React.FC = () => {
  return (
    <div
      className="code-workbench-page code-topology-workbench"
      data-testid="topology-workbench-page"
      style={{ height: '100%', width: '100%', padding: 0 }}
    >
      <ArchTopologyTabPane />
    </div>
  );
};

export default TopologyWorkbenchPage;
