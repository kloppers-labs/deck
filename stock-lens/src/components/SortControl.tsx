import { describeSort, isSortKey, sortLabel } from "../lib/inventory";
import type { SortDirection, SortKey, SortState } from "../lib/inventory";
import styles from "./SortControl.module.css";

interface SortControlProps {
  sort: SortState;
  onChange: (sort: SortState) => void;
}

const SORT_KEYS: readonly SortKey[] = ["price", "mileage", "year"];

function directionWord(direction: SortDirection): string {
  return direction === "asc" ? "ascending" : "descending";
}

export function SortControl({ sort, onChange }: SortControlProps) {
  const nextDirection: SortDirection = sort.direction === "asc" ? "desc" : "asc";

  return (
    <div className={styles.control}>
      <div className={styles.field}>
        <label htmlFor="sort-key">Sort by</label>
        <select
          id="sort-key"
          value={sort.key}
          onChange={(event) => {
            const key = event.target.value;
            if (isSortKey(key)) {
              onChange({ ...sort, key });
            }
          }}
        >
          {SORT_KEYS.map((key) => (
            <option key={key} value={key}>
              {sortLabel(key)}
            </option>
          ))}
        </select>
      </div>

      <button
        type="button"
        className={styles.direction}
        onClick={() => onChange({ ...sort, direction: nextDirection })}
        aria-label={`Sort direction: ${directionWord(sort.direction)}. Activate to sort ${directionWord(nextDirection)}.`}
      >
        {sort.direction === "asc" ? "Ascending ↑" : "Descending ↓"}
      </button>

      <p className={styles.summary} aria-live="polite">
        {describeSort(sort)}
      </p>
    </div>
  );
}
