import ELK from 'elkjs/lib/elk.bundled.js';
import type { ElkNode, LayoutOptions } from 'elkjs';

const elk = new ELK();

self.onmessage = async (
  event: MessageEvent<{ id: string; graph: ElkNode; options?: LayoutOptions }>
) => {
  const { id, graph, options } = event.data;
  try {
    const layouted = await elk.layout(graph, options);
    self.postMessage({ id, layouted });
  } catch (err: unknown) {
    const errMsg = err instanceof Error ? err.message : String(err);
    self.postMessage({ id, error: errMsg });
  }
};

