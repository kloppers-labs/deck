import { useMemo, useState } from "react";
import { FilterBar } from "./components/FilterBar";
import { SortControl } from "./components/SortControl";
import { VehicleGrid } from "./components/VehicleGrid";
import {
  applyInventoryView,
  DEFAULT_FILTERS,
  DEFAULT_SORT,
  getMakes,
} from "./lib/inventory";
import type { Filters, SortState } from "./lib/inventory";
import { parseInventoryState, toShareableUrl } from "./lib/url";
import type { InventoryState } from "./lib/url";
import { inventory } from "./lib/vehicles";
import styles from "./App.module.css";

export function App() {
  const [state, setState] = useState<InventoryState>(() =>
    parseInventoryState(window.location.search),
  );

  const makes = useMemo(() => getMakes(inventory.vehicles), []);
  const results = useMemo(
    () => applyInventoryView(inventory.vehicles, state.filters, state.sort),
    [state],
  );

  function applyState(next: InventoryState): void {
    setState(next);
    window.history.replaceState(
      null,
      "",
      toShareableUrl(window.location.pathname, next),
    );
  }

  function handleFiltersChange(patch: Partial<Filters>): void {
    applyState({ filters: { ...state.filters, ...patch }, sort: state.sort });
  }

  function handleSortChange(sort: SortState): void {
    applyState({ filters: state.filters, sort });
  }

  function handleReset(): void {
    applyState({ filters: DEFAULT_FILTERS, sort: DEFAULT_SORT });
  }

  if (inventory.error !== null) {
    return (
      <main className={styles.page}>
        <header className={styles.masthead}>
          <h1 className={styles.heading}>Stock Lens</h1>
        </header>
        <div className={styles.error} role="alert">
          <h2>Inventory unavailable</h2>
          <p>{inventory.error}</p>
          <p>
            Fix the data in <code>src/data/vehicles.json</code> and reload the page.
          </p>
        </div>
      </main>
    );
  }

  return (
    <main className={styles.page}>
      <header className={styles.masthead}>
        <h1 className={styles.heading}>Stock Lens</h1>
        <p className={styles.subtitle}>
          Browse and filter the dealership inventory.
        </p>
      </header>

      <FilterBar
        makes={makes}
        filters={state.filters}
        onChange={handleFiltersChange}
        onReset={handleReset}
      />

      <div className={styles.toolbar}>
        <p className={styles.count} aria-live="polite">
          <strong>{results.length}</strong> of {inventory.vehicles.length} vehicles
        </p>
        <SortControl sort={state.sort} onChange={handleSortChange} />
      </div>

      {results.length === 0 ? (
        <div className={styles.empty}>
          <h2>No vehicles match these filters</h2>
          <p>Try widening the price range or clearing a filter.</p>
          <button type="button" className={styles.emptyAction} onClick={handleReset}>
            Clear all filters
          </button>
        </div>
      ) : (
        <VehicleGrid vehicles={results} />
      )}
    </main>
  );
}
