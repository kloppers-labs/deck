import {
  DEFAULT_FILTERS,
  DEFAULT_SORT,
  isSortDirection,
  isSortKey,
} from "./inventory";
import type { Filters, SortState } from "./inventory";
import { FUEL_TYPES } from "./types";
import type { FuelType } from "./types";

export interface InventoryState {
  readonly filters: Filters;
  readonly sort: SortState;
}

function parseNonNegativeNumber(value: string | null): number | null {
  if (value === null || value.trim() === "") {
    return null;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
}

function parseFuelTypes(value: string | null): FuelType[] {
  if (value === null || value.trim() === "") {
    return [];
  }
  const requested = value.split(",");
  // Iterate the canonical list so the result is de-duplicated and stable.
  return FUEL_TYPES.filter((fuel) => requested.some((entry) => entry === fuel));
}

/**
 * Parses a URL query string (with or without a leading "?") into filters and
 * sort. Unknown or malformed values fall back to defaults rather than throwing.
 */
export function parseInventoryState(search: string): InventoryState {
  const params = new URLSearchParams(search);
  const sortKey = params.get("sort");
  const sortDirection = params.get("dir");

  return {
    filters: {
      make: params.get("make") ?? DEFAULT_FILTERS.make,
      fuelTypes: parseFuelTypes(params.get("fuel")),
      minPrice: parseNonNegativeNumber(params.get("min")),
      maxPrice: parseNonNegativeNumber(params.get("max")),
      search: params.get("q") ?? DEFAULT_FILTERS.search,
    },
    sort: {
      key: sortKey !== null && isSortKey(sortKey) ? sortKey : DEFAULT_SORT.key,
      direction:
        sortDirection !== null && isSortDirection(sortDirection)
          ? sortDirection
          : DEFAULT_SORT.direction,
    },
  };
}

/**
 * Serialises filters and sort into a query string. Default values are omitted
 * so shared links stay short and readable.
 */
export function serializeInventoryState(state: InventoryState): string {
  const params = new URLSearchParams();
  const { filters, sort } = state;

  if (filters.make !== "") {
    params.set("make", filters.make);
  }
  if (filters.fuelTypes.length > 0) {
    params.set("fuel", filters.fuelTypes.join(","));
  }
  if (filters.minPrice !== null) {
    params.set("min", String(filters.minPrice));
  }
  if (filters.maxPrice !== null) {
    params.set("max", String(filters.maxPrice));
  }
  const search = filters.search.trim();
  if (search !== "") {
    params.set("q", search);
  }
  if (sort.key !== DEFAULT_SORT.key) {
    params.set("sort", sort.key);
  }
  if (sort.direction !== DEFAULT_SORT.direction) {
    params.set("dir", sort.direction);
  }

  return params.toString();
}

/** Serialises state into a path+query suitable for `history.replaceState`. */
export function toShareableUrl(pathname: string, state: InventoryState): string {
  const query = serializeInventoryState(state);
  return query === "" ? pathname : `${pathname}?${query}`;
}
