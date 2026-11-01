import React from 'react';
import {
  RecordingStartDrawer,
  type RecordingStartDrawerProps,
} from './RecordingStartDrawer';

export type RecordingStartModalProps = RecordingStartDrawerProps;

/**
 * @deprecated 请统一迁移至 RecordingStartDrawer
 */
export const RecordingStartModal: React.FC<RecordingStartModalProps> = (props) => {
  return <RecordingStartDrawer {...props} />;
};

export default RecordingStartModal;
