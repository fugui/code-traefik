import React from 'react';
import { ArchTopologyTabPane } from '../ArchitectureWorkbenchPage/ArchTopologyTabPane';

export const TopologyWorkbenchPage: React.FC = () => {
  return (
    <div
      className="code-workbench-page code-topology-workbench"
      data-testid="topology-workbench-page"
    >
      <ArchTopologyTabPane />
    </div>
  );
};

export default TopologyWorkbenchPage;
