import { describe, it, expect, vi } from 'vitest';
import {
  buildTopologySvgString,
  resolveExportPalette,
  exportTopologyAsSvg,
  exportTopologyAsPng,
} from '../components/ArchCanvas/export/exportTopology';
import type { ArchCanvasNode, ArchCanvasEdge } from '../components/ArchCanvas/layout/layoutProcessor';

describe('M22-P3 F-32 Pure Vector SVG & Offscreen 2x Canvas PNG Topology Export', () => {
  const sampleNodes: ArchCanvasNode[] = [
    {
      id: 'sub_1',
      type: 'containerNode',
      position: { x: 20, y: 20 },
      width: 520,
      height: 220,
      data: {
        isExpanded: true,
        childCount: 2,
        graphNode: {
          id: 'sub_1',
          type: 'SUBSYSTEM',
          name: '交易&支付<核心>子系统',
          code: 'SUB_TRADE',
          depth: 0,
          ancestors: [],
          sort_order: 1,
          meta: { layer_level: 2, effective_layer_level: 2 },
        },
      },
    },
    {
      id: 'mod_10',
      type: 'moduleCard',
      parentId: 'sub_1',
      position: { x: 30, y: 70 },
      width: 210,
      height: 110,
      data: {
        isExpanded: false,
        graphNode: {
          id: 'mod_10',
          type: 'MODULE',
          parent_id: 'sub_1',
          name: '订单网关 & 路由',
          code: 'MOD_GATEWAY',
          depth: 1,
          ancestors: ['sub_1'],
          sort_order: 1,
          meta: {
            module_kind: 'BUSINESS',
            layer_level: 3,
            effective_layer_level: 3,
            provide_count: 1,
          },
        },
      },
    },
    {
      id: 'mod_20',
      type: 'moduleCard',
      parentId: 'sub_1',
      position: { x: 280, y: 70 },
      width: 210,
      height: 110,
      data: {
        isExpanded: false,
        graphNode: {
          id: 'mod_20',
          type: 'MODULE',
          parent_id: 'sub_1',
          name: '账户接口契约库',
          code: 'LIB_ACCOUNT',
          depth: 1,
          ancestors: ['sub_1'],
          sort_order: 2,
          meta: {
            module_kind: 'CONTRACT_LIB',
            layer_level: 1,
            effective_layer_level: 1,
          },
        },
      },
    },
  ];

  const sampleEdges: ArchCanvasEdge[] = [
    {
      id: 'edge_1',
      source: 'mod_10',
      target: 'mod_20',
      type: 'rollupEdge',
      data: {
        id: 'edge_1',
        source: 'mod_10',
        target: 'mod_20',
        kind: 'DIRECT',
        status: 'VERIFIED',
        totalCalls: 12,
        services: [],
        rawEdges: [],
      },
    },
    {
      id: 'edge_2',
      source: 'mod_20',
      target: 'mod_10',
      type: 'rollupEdge',
      data: {
        id: 'edge_2',
        source: 'mod_20',
        target: 'mod_10',
        kind: 'REALIZATION',
        services: [],
        rawEdges: [],
      },
    },
  ];

  it('generates standalone pure vector SVG with resolved Light/Dark theme tokens, escaped labels, and zero foreignObject', () => {
    const lightPalette = resolveExportPalette((tokenName) => {
      const lightTokens: Record<string, string> = {
        '--color-bg-canvas': 'rgb(248, 250, 252)',
        '--color-bg-surface': 'rgb(255, 255, 255)',
        '--color-bg-muted': 'rgb(241, 245, 249)',
        '--color-text-primary': 'rgb(15, 23, 42)',
        '--color-text-secondary': 'rgb(71, 85, 105)',
        '--color-text-muted': 'rgb(100, 116, 139)',
        '--color-border-primary': 'rgb(203, 213, 225)',
        '--color-primary': 'rgb(37, 99, 235)',
        '--color-success': 'rgb(16, 185, 129)',
        '--color-warning': 'rgb(245, 158, 11)',
        '--color-danger': 'rgb(239, 68, 68)',
        '--color-info': 'rgb(14, 165, 233)',
      };
      return lightTokens[tokenName] || '';
    });

    const darkPalette = resolveExportPalette((tokenName) => {
      const darkTokens: Record<string, string> = {
        '--color-bg-canvas': 'rgb(15, 23, 42)',
        '--color-bg-surface': 'rgb(30, 41, 59)',
        '--color-bg-muted': 'rgb(51, 65, 85)',
        '--color-text-primary': 'rgb(248, 250, 252)',
        '--color-text-secondary': 'rgb(203, 213, 225)',
        '--color-text-muted': 'rgb(148, 163, 184)',
        '--color-border-primary': 'rgb(71, 85, 105)',
        '--color-primary': 'rgb(96, 165, 250)',
        '--color-success': 'rgb(52, 211, 153)',
        '--color-warning': 'rgb(251, 191, 36)',
        '--color-danger': 'rgb(248, 113, 113)',
        '--color-info': 'rgb(56, 189, 248)',
      };
      return darkTokens[tokenName] || '';
    });

    const lightResult = buildTopologySvgString(sampleNodes, sampleEdges, { palette: lightPalette });
    const darkResult = buildTopologySvgString(sampleNodes, sampleEdges, { palette: darkPalette });

    expect(lightResult.svg).toContain('<svg ');
    expect(lightResult.svg).not.toContain('<foreignObject');
    expect(lightResult.svg).not.toContain('var(--');
    expect(lightResult.svg).toContain('交易&amp;支付&lt;核心&gt;子系统');
    expect(lightResult.svg).toContain('L3');
    expect(lightResult.svg).toContain('«interface»');
    expect(lightResult.svg).toContain('<marker ');
    expect(lightResult.svg).toContain('stroke-dasharray="4 4"');
    expect(lightResult.svg).toContain('rgb(255, 255, 255)');

    expect(darkResult.svg).toContain('rgb(15, 23, 42)');
    expect(darkResult.svg).toContain('rgb(30, 41, 59)');
    expect(darkResult.svg).not.toContain('<foreignObject');
  });

  it('downloads SVG and 2x offscreen Canvas PNG locally with zero backend HTTP calls', async () => {
    const fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);

    const downloaded: Array<{ href: string; download: string }> = [];
    const createdBlobs: Blob[] = [];
    const scaleSpy = vi.fn();
    const drawImageSpy = vi.fn();

    const fakeCanvas = {
      width: 0,
      height: 0,
      getContext: () => ({
        scale: scaleSpy,
        drawImage: drawImageSpy,
      }),
      toBlob: (cb: (blob: Blob | null) => void) => {
        const pngBlob = new Blob(['png-bytes'], { type: 'image/png' });
        createdBlobs.push(pngBlob);
        cb(pngBlob);
      },
    };

    const fakeDoc = {
      documentElement: {},
      createElement: (tag: string) => {
        if (tag === 'canvas') return fakeCanvas;
        return {
          href: '',
          download: '',
          click() {
            downloaded.push({ href: this.href, download: this.download });
          },
        };
      },
      body: {
        appendChild: vi.fn(),
        removeChild: vi.fn(),
      },
    };

    const createObjectUrl = vi.fn((blob: Blob) => {
      createdBlobs.push(blob);
      return `blob:mock/${createdBlobs.length}`;
    });
    const revokeObjectUrl = vi.fn();

    class MockImage {
      onload: (() => void) | null = null;
      onerror: ((err: unknown) => void) | null = null;
      set src(_v: string) {
        if (this.onload) this.onload();
      }
    }

    const svgRes = exportTopologyAsSvg(sampleNodes, sampleEdges, {
      fileNamePrefix: 'arch-logical',
      documentRef: fakeDoc as unknown as Document,
      urlApi: { createObjectURL: createObjectUrl, revokeObjectURL: revokeObjectUrl },
    });
    expect(svgRes.width).toBeGreaterThan(0);
    expect(downloaded[0].download).toMatch(/^arch-logical-.*\.svg$/);

    await exportTopologyAsPng(sampleNodes, sampleEdges, {
      fileNamePrefix: 'arch-logical',
      scale: 2,
      documentRef: fakeDoc as unknown as Document,
      urlApi: { createObjectURL: createObjectUrl, revokeObjectURL: revokeObjectUrl },
      createImage: () => new MockImage() as unknown as HTMLImageElement,
    });

    expect(scaleSpy).toHaveBeenCalledWith(2, 2);
    expect(drawImageSpy).toHaveBeenCalledTimes(1);
    expect(downloaded[1].download).toMatch(/^arch-logical-.*\.png$/);
    expect(fetchSpy).not.toHaveBeenCalled();

    vi.unstubAllGlobals();
  });
});
