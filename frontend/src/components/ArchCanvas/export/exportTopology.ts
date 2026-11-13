import { resolveEdgeVisualStyle, type ArchCanvasEdge, type ArchCanvasNode } from '../layout/layoutProcessor';

export interface TopologyExportPalette {
  canvasBg: string;
  surfaceBg: string;
  mutedBg: string;
  textPrimary: string;
  textSecondary: string;
  textMuted: string;
  textInverse: string;
  borderPrimary: string;
  primary: string;
  success: string;
  warning: string;
  danger: string;
  info: string;
}

export interface BuildSvgOptions {
  padding?: number;
  palette?: TopologyExportPalette;
  title?: string;
}

export interface ExportSvgResult {
  svg: string;
  width: number;
  height: number;
}

export interface ExportActionOptions extends BuildSvgOptions {
  fileNamePrefix?: string;
  scale?: number;
  documentRef?: Document;
  urlApi?: Pick<typeof URL, 'createObjectURL' | 'revokeObjectURL'>;
  createImage?: () => HTMLImageElement;
}

interface AbsBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

// Node/无 DOM 环境兜底中性色（采用 W3C HSL 格式；浏览器内 100% 由 getComputedStyle 读取主题 --color-* 变量覆盖）
const FALLBACK_PALETTE: TopologyExportPalette = {
  canvasBg: 'hsl(210, 40%, 98%)',
  surfaceBg: 'hsl(0, 0%, 100%)',
  mutedBg: 'hsl(210, 40%, 96%)',
  textPrimary: 'hsl(222, 47%, 11%)',
  textSecondary: 'hsl(215, 25%, 27%)',
  textMuted: 'hsl(215, 16%, 47%)',
  textInverse: 'hsl(0, 0%, 100%)',
  borderPrimary: 'hsl(213, 27%, 84%)',
  primary: 'hsl(221, 83%, 53%)',
  success: 'hsl(160, 84%, 39%)',
  warning: 'hsl(38, 92%, 50%)',
  danger: 'hsl(0, 84%, 60%)',
  info: 'hsl(199, 89%, 48%)',
};

/**
 * 解析当前文档根节点上的 --color-* 语义化主题色 Token，生成独立的绝对色值调色板
 */
export function resolveExportPalette(readCssVar?: (name: string) => string): TopologyExportPalette {
  const reader =
    readCssVar ??
    ((tokenName: string): string => {
      if (typeof document === 'undefined' || typeof getComputedStyle !== 'function') {
        return '';
      }
      return getComputedStyle(document.documentElement).getPropertyValue(tokenName).trim();
    });

  const pick = (tokenName: string, fallback: string, altTokenName?: string): string => {
    const val = reader(tokenName);
    if (val) return val;
    if (altTokenName) {
      const alt = reader(altTokenName);
      if (alt) return alt;
    }
    return fallback;
  };

  return {
    canvasBg: pick('--color-bg-app', FALLBACK_PALETTE.canvasBg, '--color-bg-canvas'),
    surfaceBg: pick('--color-bg-surface', FALLBACK_PALETTE.surfaceBg),
    mutedBg: pick('--color-bg-muted', FALLBACK_PALETTE.mutedBg),
    textPrimary: pick('--color-text-primary', FALLBACK_PALETTE.textPrimary),
    textSecondary: pick('--color-text-secondary', FALLBACK_PALETTE.textSecondary),
    textMuted: pick('--color-text-muted', FALLBACK_PALETTE.textMuted),
    textInverse: pick('--color-text-white', FALLBACK_PALETTE.textInverse),
    borderPrimary: pick('--color-border-primary', FALLBACK_PALETTE.borderPrimary),
    primary: pick('--color-primary', FALLBACK_PALETTE.primary),
    success: pick('--color-success', FALLBACK_PALETTE.success),
    warning: pick('--color-warning', FALLBACK_PALETTE.warning),
    danger: pick('--color-danger', FALLBACK_PALETTE.danger),
    info: pick('--color-info', FALLBACK_PALETTE.info),
  };
}

function escapeXml(raw: string): string {
  return raw
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function truncateLabel(text: string, maxChars: number): string {
  if (text.length <= maxChars) return text;
  return `${text.slice(0, Math.max(1, maxChars - 1))}…`;
}

function resolveCssVarColor(colorExpr: string, palette: TopologyExportPalette): string {
  if (colorExpr.includes('--color-danger')) return palette.danger;
  if (colorExpr.includes('--color-warning')) return palette.warning;
  if (colorExpr.includes('--color-success')) return palette.success;
  if (colorExpr.includes('--color-info')) return palette.info;
  if (colorExpr.includes('--color-text-muted')) return palette.textMuted;
  if (colorExpr.includes('--color-text-secondary')) return palette.textSecondary;
  if (colorExpr.includes('--color-border-hover')) return palette.borderPrimary;
  if (colorExpr.includes('--color-primary')) return palette.primary;
  if (colorExpr.startsWith('var(')) return palette.primary;
  return colorExpr;
}

function readNodeSize(node: ArchCanvasNode): { width: number; height: number } {
  const isContainer = node.type === 'containerNode';
  const defaultW = isContainer ? 320 : 210;
  const defaultH = isContainer ? 160 : 96;
  const styleWidth = typeof node.style?.width === 'number' ? node.style.width : undefined;
  const styleHeight = typeof node.style?.height === 'number' ? node.style.height : undefined;
  return {
    width: node.measured?.width ?? node.width ?? styleWidth ?? defaultW,
    height: node.measured?.height ?? node.height ?? styleHeight ?? defaultH,
  };
}

function computeAbsoluteBoundsMap(nodes: ArchCanvasNode[]): Map<string, AbsBounds> {
  const byId = new Map<string, ArchCanvasNode>();
  for (const n of nodes) {
    byId.set(n.id, n);
  }

  const cache = new Map<string, AbsBounds>();
  const visiting = new Set<string>();

  const resolve = (nodeId: string): AbsBounds | null => {
    const existing = cache.get(nodeId);
    if (existing) return existing;
    if (visiting.has(nodeId)) return null;

    const node = byId.get(nodeId);
    if (!node) return null;
    visiting.add(nodeId);

    const { width, height } = readNodeSize(node);
    let absX = node.position?.x ?? 0;
    let absY = node.position?.y ?? 0;

    if (node.parentId && byId.has(node.parentId)) {
      const parentBounds = resolve(node.parentId);
      if (parentBounds) {
        absX += parentBounds.x;
        absY += parentBounds.y;
      }
    }

    visiting.delete(nodeId);
    const bounds: AbsBounds = { x: absX, y: absY, width, height };
    cache.set(nodeId, bounds);
    return bounds;
  };

  for (const n of nodes) {
    resolve(n.id);
  }
  return cache;
}

function renderContainerSvg(
  node: ArchCanvasNode,
  bounds: AbsBounds,
  offsetX: number,
  offsetY: number,
  palette: TopologyExportPalette
): string {
  const x = Math.round(bounds.x + offsetX);
  const y = Math.round(bounds.y + offsetY);
  const w = Math.round(bounds.width);
  const h = Math.round(bounds.height);
  const gNode = node.data.graphNode;
  const effLayer = Number(gNode.meta?.effective_layer_level ?? gNode.meta?.layer_level ?? 0);
  const titleParts = [gNode.code, gNode.name].filter(Boolean);
  const headerTitle = truncateLabel(titleParts.join(' · '), Math.max(12, Math.floor((w - 60) / 7)));
  const isExternal = gNode.type === 'EXTERNAL_CONTAINER';

  const parts: string[] = [
    `<g data-node-id="${escapeXml(node.id)}">`,
    `<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="10" ry="10" fill="${escapeXml(
      palette.mutedBg
    )}" fill-opacity="0.55" stroke="${escapeXml(palette.borderPrimary)}" stroke-width="1.5" ${
      isExternal ? 'stroke-dasharray="6 4"' : ''
    } />`,
    `<rect x="${x}" y="${y}" width="${w}" height="32" rx="10" ry="10" fill="${escapeXml(
      palette.surfaceBg
    )}" stroke="${escapeXml(palette.borderPrimary)}" stroke-width="1" />`,
    `<text x="${x + 12}" y="${y + 20}" font-family="system-ui, -apple-system, sans-serif" font-size="12" font-weight="600" fill="${escapeXml(
      palette.textPrimary
    )}">${escapeXml(headerTitle)}</text>`,
  ];

  if (effLayer > 0) {
    const badgeW = 34;
    const badgeX = x + w - badgeW - 10;
    parts.push(
      `<rect x="${badgeX}" y="${y + 7}" width="${badgeW}" height="18" rx="4" fill="${escapeXml(
        palette.primary
      )}" fill-opacity="0.14" stroke="${escapeXml(palette.primary)}" stroke-width="0.8" />`,
      `<text x="${badgeX + badgeW / 2}" y="${y + 19}" text-anchor="middle" font-family="system-ui, sans-serif" font-size="10" font-weight="700" fill="${escapeXml(
        palette.primary
      )}">${escapeXml(`L${effLayer}`)}</text>`
    );
  }

  parts.push('</g>');
  return parts.join('');
}

function renderLeafCardSvg(
  node: ArchCanvasNode,
  bounds: AbsBounds,
  offsetX: number,
  offsetY: number,
  palette: TopologyExportPalette
): string {
  const x = Math.round(bounds.x + offsetX);
  const y = Math.round(bounds.y + offsetY);
  const w = Math.round(bounds.width);
  const h = Math.round(bounds.height);
  const gNode = node.data.graphNode;
  const isContractLib = gNode.meta?.module_kind === 'CONTRACT_LIB';
  const isUndeployed = !isContractLib && gNode.type === 'MODULE' && gNode.meta?.is_deployed === false;
  const effLayer = Number(gNode.meta?.effective_layer_level ?? gNode.meta?.layer_level ?? 0);

  let accent = palette.primary;
  if (isContractLib) accent = palette.info;
  else if (isUndeployed) accent = palette.warning;
  else if (gNode.type === 'UNREGISTERED_INTERFACE') accent = palette.danger;

  const dashAttr = isContractLib || isUndeployed ? 'stroke-dasharray="5 3"' : '';
  const codeText = truncateLabel(gNode.code || gNode.id, Math.max(10, Math.floor((w - 64) / 7)));
  const nameText = truncateLabel(gNode.name || '', Math.max(12, Math.floor((w - 24) / 7)));

  const parts: string[] = [
    `<g data-node-id="${escapeXml(node.id)}">`,
    `<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="8" ry="8" fill="${escapeXml(
      palette.surfaceBg
    )}" stroke="${escapeXml(accent)}" stroke-width="1.5" ${dashAttr} />`,
    `<text x="${x + 12}" y="${y + 22}" font-family="monospace, system-ui" font-size="11" font-weight="700" fill="${escapeXml(
      palette.textSecondary
    )}">${escapeXml(codeText)}</text>`,
    `<text x="${x + 12}" y="${y + 44}" font-family="system-ui, -apple-system, sans-serif" font-size="13" font-weight="600" fill="${escapeXml(
      palette.textPrimary
    )}">${escapeXml(nameText)}</text>`,
  ];

  let tagX = x + 12;
  const tagY = y + h - 26;
  if (isContractLib) {
    parts.push(
      `<rect x="${tagX}" y="${tagY}" width="72" height="17" rx="4" fill="${escapeXml(
        palette.mutedBg
      )}" stroke="${escapeXml(palette.info)}" stroke-width="0.8" />`,
      `<text x="${tagX + 36}" y="${tagY + 12}" text-anchor="middle" font-family="system-ui, sans-serif" font-size="10" font-weight="600" fill="${escapeXml(
        palette.info
      )}">${escapeXml('«interface»')}</text>`
    );
    tagX += 78;
  }

  if (effLayer > 0) {
    parts.push(
      `<rect x="${tagX}" y="${tagY}" width="30" height="17" rx="4" fill="${escapeXml(
        palette.primary
      )}" fill-opacity="0.14" stroke="${escapeXml(palette.primary)}" stroke-width="0.8" />`,
      `<text x="${tagX + 15}" y="${tagY + 12}" text-anchor="middle" font-family="system-ui, sans-serif" font-size="10" font-weight="700" fill="${escapeXml(
        palette.primary
      )}">${escapeXml(`L${effLayer}`)}</text>`
    );
  }

  parts.push('</g>');
  return parts.join('');
}

function renderEdgesSvg(
  edges: ArchCanvasEdge[],
  boundsMap: Map<string, AbsBounds>,
  offsetX: number,
  offsetY: number,
  palette: TopologyExportPalette
): { defs: string; paths: string } {
  const markerMap = new Map<string, string>();
  const paths: string[] = [];

  const getMarkerId = (color: string): string => {
    const existing = markerMap.get(color);
    if (existing) return existing;
    const id = `arch-arrow-${markerMap.size + 1}`;
    markerMap.set(color, id);
    return id;
  };

  for (const edge of edges) {
    const src = boundsMap.get(edge.source);
    const dst = boundsMap.get(edge.target);
    if (!src || !dst) continue;

    const sx = src.x + src.width + offsetX;
    const sy = src.y + src.height / 2 + offsetY;
    const tx = dst.x + offsetX;
    const ty = dst.y + dst.height / 2 + offsetY;

    const bendPoints = Array.isArray(edge.data?.bendPoints) ? edge.data.bendPoints : [];
    const dx = Math.max(40, Math.abs(tx - sx) * 0.45);
    const style = resolveEdgeVisualStyle(edge.data);
    const stroke = resolveCssVarColor(style.strokeColor, palette);
    const markerId = getMarkerId(stroke);
    const dashAttr = style.strokeDasharray ? `stroke-dasharray="${escapeXml(style.strokeDasharray)}"` : '';
    const strokeWidth = edge.data?.isCycle ? 2.4 : 1.7;

    const d =
      bendPoints.length > 0
        ? [
            `M ${Math.round(sx)} ${Math.round(sy)}`,
            ...bendPoints.map((bp) => `L ${Math.round(bp.x + offsetX)} ${Math.round(bp.y + offsetY)}`),
            `L ${Math.round(tx)} ${Math.round(ty)}`,
          ].join(' ')
        : `M ${Math.round(sx)} ${Math.round(sy)} C ${Math.round(sx + dx)} ${Math.round(sy)}, ${Math.round(
            tx - dx
          )} ${Math.round(ty)}, ${Math.round(tx)} ${Math.round(ty)}`;

    paths.push(
      `<path d="${d}" fill="none" stroke="${escapeXml(stroke)}" stroke-width="${strokeWidth}" ${dashAttr} marker-end="url(#${markerId})" />`
    );

    const edgeTotalCalls =
      typeof edge.data?.totalCalls === 'number'
        ? edge.data.totalCalls
        : typeof edge.data?.callCount === 'number'
          ? edge.data.callCount
          : 0;

    if (edgeTotalCalls > 0) {
      const mx = Math.round((sx + tx) / 2);
      const my = Math.round((sy + ty) / 2) - 4;
      const labelText = `${edgeTotalCalls}`;
      const pillW = Math.max(22, labelText.length * 7 + 10);
      paths.push(
        `<rect x="${mx - pillW / 2}" y="${my - 9}" width="${pillW}" height="16" rx="8" fill="${escapeXml(
          palette.surfaceBg
        )}" stroke="${escapeXml(stroke)}" stroke-width="0.9" />`,
        `<text x="${mx}" y="${my + 2}" text-anchor="middle" font-family="system-ui, sans-serif" font-size="10" font-weight="600" fill="${escapeXml(
          palette.textPrimary
        )}">${escapeXml(labelText)}</text>`
      );
    }
  }

  const markerDefs: string[] = [];
  markerMap.forEach((id, color) => {
    markerDefs.push(
      `<marker id="${id}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M 0 1 L 10 5 L 0 9 z" fill="${escapeXml(
        color
      )}" /></marker>`
    );
  });

  return {
    defs: `<defs>${markerDefs.join('')}</defs>`,
    paths: paths.join(''),
  };
}

/**
 * 纯矢量编程式构建架构拓扑 SVG 字符串（严禁使用 foreignObject，全部 CSS 变量即时解析为实体色值）
 */
export function buildTopologySvgString(
  nodes: ArchCanvasNode[],
  edges: ArchCanvasEdge[],
  options: BuildSvgOptions = {}
): ExportSvgResult {
  const padding = options.padding ?? 32;
  const palette = options.palette ?? resolveExportPalette();
  const boundsMap = computeAbsoluteBoundsMap(nodes);

  if (boundsMap.size === 0) {
    const emptyW = 480;
    const emptyH = 220;
    const svg = [
      `<svg xmlns="http://www.w3.org/2000/svg" width="${emptyW}" height="${emptyH}" viewBox="0 0 ${emptyW} ${emptyH}">`,
      `<rect x="0" y="0" width="${emptyW}" height="${emptyH}" fill="${escapeXml(palette.canvasBg)}" />`,
      `<text x="${emptyW / 2}" y="${emptyH / 2}" text-anchor="middle" font-family="system-ui, sans-serif" font-size="14" fill="${escapeXml(
        palette.textSecondary
      )}">${escapeXml(options.title || '当前画布暂无可导出的拓扑节点')}</text>`,
      '</svg>',
    ].join('');
    return { svg, width: emptyW, height: emptyH };
  }

  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;

  boundsMap.forEach((b) => {
    if (b.x < minX) minX = b.x;
    if (b.y < minY) minY = b.y;
    if (b.x + b.width > maxX) maxX = b.x + b.width;
    if (b.y + b.height > maxY) maxY = b.y + b.height;
  });

  const width = Math.max(320, Math.ceil(maxX - minX + padding * 2));
  const height = Math.max(200, Math.ceil(maxY - minY + padding * 2));
  const offsetX = -minX + padding;
  const offsetY = -minY + padding;

  const containerNodes = nodes.filter((n) => n.type === 'containerNode');
  const leafNodes = nodes.filter((n) => n.type !== 'containerNode');

  const containersSvg = containerNodes
    .map((n) => {
      const b = boundsMap.get(n.id);
      return b ? renderContainerSvg(n, b, offsetX, offsetY, palette) : '';
    })
    .join('');

  const { defs, paths: edgesSvg } = renderEdgesSvg(edges, boundsMap, offsetX, offsetY, palette);

  const leavesSvg = leafNodes
    .map((n) => {
      const b = boundsMap.get(n.id);
      return b ? renderLeafCardSvg(n, b, offsetX, offsetY, palette) : '';
    })
    .join('');

  const svg = [
    `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">`,
    defs,
    `<rect x="0" y="0" width="${width}" height="${height}" fill="${escapeXml(palette.canvasBg)}" />`,
    `<g data-layer="containers">${containersSvg}</g>`,
    `<g data-layer="edges">${edgesSvg}</g>`,
    `<g data-layer="cards">${leavesSvg}</g>`,
    '</svg>',
  ].join('');

  return { svg, width, height };
}

function buildExportTimestamp(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(
    d.getMinutes()
  )}${pad(d.getSeconds())}`;
}

function triggerBlobDownload(
  blob: Blob,
  filename: string,
  documentRef: Document,
  urlApi: Pick<typeof URL, 'createObjectURL' | 'revokeObjectURL'>
): void {
  const objectUrl = urlApi.createObjectURL(blob);
  const anchor = documentRef.createElement('a');
  anchor.href = objectUrl;
  anchor.download = filename;
  documentRef.body.appendChild(anchor);
  anchor.click();
  documentRef.body.removeChild(anchor);
  urlApi.revokeObjectURL(objectUrl);
}

/**
 * 纯前端下载矢量 SVG 拓扑文件（零后端 HTTP 请求）
 */
export function exportTopologyAsSvg(
  nodes: ArchCanvasNode[],
  edges: ArchCanvasEdge[],
  options: ExportActionOptions = {}
): ExportSvgResult {
  const doc = options.documentRef ?? document;
  const urlApi = options.urlApi ?? URL;
  const result = buildTopologySvgString(nodes, edges, options);
  const blob = new Blob([result.svg], { type: 'image/svg+xml;charset=utf-8' });
  const prefix = options.fileNamePrefix || 'architecture-topology';
  const filename = `${prefix}-${buildExportTimestamp()}.svg`;
  triggerBlobDownload(blob, filename, doc, urlApi);
  return result;
}

/**
 * 将纯矢量 SVG 通过 2x 离屏 Canvas 光栅化并本地下载 PNG（零后端 HTTP 请求）
 */
export async function exportTopologyAsPng(
  nodes: ArchCanvasNode[],
  edges: ArchCanvasEdge[],
  options: ExportActionOptions = {}
): Promise<ExportSvgResult> {
  const doc = options.documentRef ?? document;
  const urlApi = options.urlApi ?? URL;
  const scale = options.scale ?? 2;
  const result = buildTopologySvgString(nodes, edges, options);
  const svgBlob = new Blob([result.svg], { type: 'image/svg+xml;charset=utf-8' });
  const svgObjectUrl = urlApi.createObjectURL(svgBlob);

  try {
    const pngBlob = await new Promise<Blob>((resolve, reject) => {
      const img = options.createImage ? options.createImage() : new Image();
      img.onload = () => {
        const canvas = doc.createElement('canvas');
        canvas.width = Math.max(1, Math.round(result.width * scale));
        canvas.height = Math.max(1, Math.round(result.height * scale));
        const ctx = canvas.getContext('2d');
        if (!ctx) {
          reject(new Error('无法创建 2D 离屏画布上下文'));
          return;
        }
        ctx.scale(scale, scale);
        ctx.drawImage(img, 0, 0);
        canvas.toBlob((blob) => {
          if (!blob) {
            reject(new Error('离屏 Canvas PNG 编码失败'));
            return;
          }
          resolve(blob);
        }, 'image/png');
      };
      img.onerror = () => {
        reject(new Error('SVG 矢量图加载失败，无法转为 PNG'));
      };
      img.src = svgObjectUrl;
    });

    const prefix = options.fileNamePrefix || 'architecture-topology';
    const filename = `${prefix}-${buildExportTimestamp()}.png`;
    triggerBlobDownload(pngBlob, filename, doc, urlApi);
    return result;
  } finally {
    urlApi.revokeObjectURL(svgObjectUrl);
  }
}
