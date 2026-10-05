import type { FuelType, Vehicle } from "./types";

export interface Filters {
  /** Exact make match; empty string means "all makes". */
  readonly make: string;
  /** Empty array means "all fuel types". */
  readonly fuelTypes: readonly FuelType[];
  readonly minPrice: number | null;
  readonly maxPrice: number | null;
  /** Case-insensitive free text matched against "make model". */
  readonly search: string;
}

export type SortKey = "price" | "mileage" | "year";
export type SortDirection = "asc" | "desc";

export interface SortState {
  readonly key: SortKey;
  readonly direction: SortDirection;
}

export const DEFAULT_FILTERS: Filters = {
  make: "",
  fuelTypes: [],
  minPrice: null,
  maxPrice: null,
  search: "",
};

export const DEFAULT_SORT: SortState = {
  key: "price",
  direction: "asc",
};

export const SORT_KEYS: readonly SortKey[] = ["price", "mileage", "year"];
export const SORT_DIRECTIONS: readonly SortDirection[] = ["asc", "desc"];

export function isSortKey(value: string): value is SortKey {
  return SORT_KEYS.some((key) => key === value);
}

export function isSortDirection(value: string): value is SortDirection {
  return SORT_DIRECTIONS.some((direction) => direction === value);
}

/**
 * Returns the subset of `vehicles` matching every active filter. Pure: the
 * input array is never mutated.
 */
export function filterVehicles(
  vehicles: readonly Vehicle[],
  filters: Filters,
): Vehicle[] {
  const search = filters.search.trim().toLowerCase();

  return vehicles.filter((vehicle) => {
    if (filters.make !== "" && vehicle.make !== filters.make) {
      return false;
    }
    if (
      filters.fuelTypes.length > 0 &&
      !filters.fuelTypes.includes(vehicle.fuelType)
    ) {
      return false;
    }
    if (filters.minPrice !== null && vehicle.priceZar < filters.minPrice) {
      return false;
    }
    if (filters.maxPrice !== null && vehicle.priceZar > filters.maxPrice) {
      return false;
    }
    if (
      search !== "" &&
      !`${vehicle.make} ${vehicle.model}`.toLowerCase().includes(search)
    ) {
      return false;
    }
    return true;
  });
}

function compareBy(key: SortKey, a: Vehicle, b: Vehicle): number {
  switch (key) {
    case "price":
      return a.priceZar - b.priceZar;
    case "mileage":
      return a.mileageKm - b.mileageKm;
    case "year":
      return a.year - b.year;
  }
}

/**
 * Returns a new array sorted by `sort.key`/`sort.direction`. Ties fall back to
 * `id` so ordering is deterministic. Pure: the input array is never mutated.
 */
export function sortVehicles(
  vehicles: readonly Vehicle[],
  sort: SortState,
): Vehicle[] {
  const factor = sort.direction === "asc" ? 1 : -1;

  return [...vehicles].sort((a, b) => {
    const primary = compareBy(sort.key, a, b) * factor;
    if (primary !== 0) {
      return primary;
    }
    return a.id.localeCompare(b.id);
  });
}

/** Filter then sort, without mutating either input. */
export function applyInventoryView(
  vehicles: readonly Vehicle[],
  filters: Filters,
  sort: SortState,
): Vehicle[] {
  return sortVehicles(filterVehicles(vehicles, filters), sort);
}

/** Unique makes present in the inventory, alphabetically sorted. */
export function getMakes(vehicles: readonly Vehicle[]): string[] {
  const makes = new Set<string>();
  for (const vehicle of vehicles) {
    makes.add(vehicle.make);
  }
  return [...makes].sort((a, b) => a.localeCompare(b));
}

/** Add or remove a fuel type from a selection, preserving canonical order. */
export function toggleFuelType(
  fuelTypes: readonly FuelType[],
  fuelType: FuelType,
): FuelType[] {
  return fuelTypes.includes(fuelType)
    ? fuelTypes.filter((entry) => entry !== fuelType)
    : [...fuelTypes, fuelType];
}

const SORT_LABELS: Record<SortKey, string> = {
  price: "Price",
  mileage: "Mileage",
  year: "Year",
};

export function sortLabel(key: SortKey): string {
  return SORT_LABELS[key];
}

export function describeSort(sort: SortState): string {
  const direction = sort.direction === "asc" ? "ascending" : "descending";
  return `Sorted by ${SORT_LABELS[sort.key]}, ${direction}`;
}
