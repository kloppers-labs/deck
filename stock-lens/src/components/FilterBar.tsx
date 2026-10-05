import { toggleFuelType } from "../lib/inventory";
import type { Filters } from "../lib/inventory";
import { FUEL_TYPES } from "../lib/types";
import styles from "./FilterBar.module.css";

interface FilterBarProps {
  makes: readonly string[];
  filters: Filters;
  onChange: (patch: Partial<Filters>) => void;
  onReset: () => void;
}

function parsePrice(value: string): number | null {
  if (value.trim() === "") {
    return null;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

export function FilterBar({ makes, filters, onChange, onReset }: FilterBarProps) {
  const hasActiveFilters =
    filters.make !== "" ||
    filters.fuelTypes.length > 0 ||
    filters.minPrice !== null ||
    filters.maxPrice !== null ||
    filters.search.trim() !== "";

  return (
    <section className={styles.bar} aria-label="Inventory filters">
      <div className={styles.field}>
        <label htmlFor="filter-make">Make</label>
        <select
          id="filter-make"
          value={filters.make}
          onChange={(event) => onChange({ make: event.target.value })}
        >
          <option value="">All makes</option>
          {makes.map((make) => (
            <option key={make} value={make}>
              {make}
            </option>
          ))}
        </select>
      </div>

      <fieldset className={styles.field}>
        <legend>Fuel type</legend>
        <div className={styles.checkboxGroup}>
          {FUEL_TYPES.map((fuel) => (
            <label key={fuel} className={styles.checkbox}>
              <input
                type="checkbox"
                checked={filters.fuelTypes.includes(fuel)}
                onChange={() =>
                  onChange({
                    fuelTypes: toggleFuelType(filters.fuelTypes, fuel),
                  })
                }
              />
              <span>{capitalize(fuel)}</span>
            </label>
          ))}
        </div>
      </fieldset>

      <div className={styles.field}>
        <label htmlFor="filter-min-price">Min price (R)</label>
        <input
          id="filter-min-price"
          type="number"
          min={0}
          step={1000}
          inputMode="numeric"
          value={filters.minPrice ?? ""}
          onChange={(event) => onChange({ minPrice: parsePrice(event.target.value) })}
        />
      </div>

      <div className={styles.field}>
        <label htmlFor="filter-max-price">Max price (R)</label>
        <input
          id="filter-max-price"
          type="number"
          min={0}
          step={1000}
          inputMode="numeric"
          value={filters.maxPrice ?? ""}
          onChange={(event) => onChange({ maxPrice: parsePrice(event.target.value) })}
        />
      </div>

      <div className={styles.field}>
        <label htmlFor="filter-search">Search make or model</label>
        <input
          id="filter-search"
          type="search"
          placeholder="e.g. Toyota Hilux"
          value={filters.search}
          onChange={(event) => onChange({ search: event.target.value })}
        />
      </div>

      <div className={styles.resetWrap}>
        <button
          type="button"
          className={styles.reset}
          onClick={onReset}
          disabled={!hasActiveFilters}
        >
          Clear filters
        </button>
      </div>
    </section>
  );
}
