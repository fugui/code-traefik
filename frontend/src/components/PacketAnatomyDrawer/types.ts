import React from 'react';
import type { PacketAnatomyDrawerDTO } from '../../types';

export type PacketInspectorViewMode = 'BUSINESS' | 'EXPERT';

export interface BusinessSummaryProps {
  data: PacketAnatomyDrawerDTO;
  viewMode: PacketInspectorViewMode;
  onViewModeChange: (mode: PacketInspectorViewMode) => void;
  onFilterTrace?: (traceId: string) => void;
  onRedecode?: (recordId: string) => Promise<void>;
  extraActions?: React.ReactNode;
}

export interface BusinessPayloadCompareProps {
  data: PacketAnatomyDrawerDTO;
  onRedecode?: (recordId: string) => Promise<void>;
}

export interface RpcLatencyWaterfallProps {
  data: PacketAnatomyDrawerDTO;
}

export interface AdvancedProtocolAccordionProps {
  data: PacketAnatomyDrawerDTO;
  defaultExpanded?: boolean;
  onFilterTrace?: (traceId: string) => void;
}
