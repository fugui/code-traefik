import * as React from 'react';

function is(x: unknown, y: unknown): boolean {
  return (x === y && (x !== 0 || 1 / (x as number) === 1 / (y as number))) || (x !== x && y !== y);
}

const objectIs: (x: unknown, y: unknown) => boolean =
  typeof Object.is === 'function' ? Object.is : is;

export function useSyncExternalStore<Snapshot>(
  subscribe: (onStoreChange: () => void) => () => void,
  getSnapshot: () => Snapshot,
  getServerSnapshot?: () => Snapshot
): Snapshot {
  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

export function useSyncExternalStoreWithSelector<Snapshot, Selection>(
  subscribe: (onStoreChange: () => void) => () => void,
  getSnapshot: () => Snapshot,
  getServerSnapshot: undefined | null | (() => Snapshot),
  selector: (snapshot: Snapshot) => Selection,
  isEqual?: (a: Selection, b: Selection) => boolean
): Selection {
  const instRef = React.useRef<{ hasValue: boolean; value: Selection | null }>({
    hasValue: false,
    value: null,
  });

  const [getSelection, getServerSelection] = React.useMemo(() => {
    let hasMemo = false;
    let memoizedSnapshot: Snapshot;
    let memoizedSelection: Selection;

    function memoizedSelector(nextSnapshot: Snapshot) {
      const inst = instRef.current;
      if (!hasMemo) {
        hasMemo = true;
        memoizedSnapshot = nextSnapshot;
        const nextSelection = selector(nextSnapshot);
        if (isEqual !== undefined && inst.hasValue) {
          const currentSelection = inst.value as Selection;
          if (isEqual(currentSelection, nextSelection)) {
            memoizedSelection = currentSelection;
            return currentSelection;
          }
        }
        memoizedSelection = nextSelection;
        return nextSelection;
      }

      const currentSelection = memoizedSelection;
      if (objectIs(memoizedSnapshot, nextSnapshot)) {
        return currentSelection;
      }
      const nextSelection = selector(nextSnapshot);
      if (isEqual !== undefined && isEqual(currentSelection, nextSelection)) {
        return currentSelection;
      }
      memoizedSnapshot = nextSnapshot;
      memoizedSelection = nextSelection;
      return nextSelection;
    }

    const getSnapshotWithSelector = () => memoizedSelector(getSnapshot());
    const getServerSnapshotWithSelector =
      getServerSnapshot == null ? undefined : () => memoizedSelector(getServerSnapshot());

    return [getSnapshotWithSelector, getServerSnapshotWithSelector];
  }, [getSnapshot, getServerSnapshot, selector, isEqual]);

  const value = React.useSyncExternalStore(subscribe, getSelection, getServerSelection);

  React.useEffect(() => {
    instRef.current.hasValue = true;
    instRef.current.value = value;
  }, [value]);

  React.useDebugValue(value);
  return value;
}

export default {
  useSyncExternalStore,
  useSyncExternalStoreWithSelector,
};

