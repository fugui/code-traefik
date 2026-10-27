import React from 'react';
import ReactDOM from 'react-dom/client';
import { setupChunkErrorReloader } from '@code/common';
import App from './App';
import './index.css';

// 注册前端发版后静态 Chunk 404 自动刷新恢复机制
setupChunkErrorReloader();

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App isEmbedded={false} />
  </React.StrictMode>,
);
