import { describe, expect, it } from "vitest";
import {
  applyInventoryView,
  DEFAULT_FILTERS,
  DEFAULT_SORT,
  filterVehicles,
  getMakes,
  sortVehicles,
  toggleFuelType,
} from "./inventory";
import type { Filters, SortState } from "./inventory";
import type { Vehicle } from "./types";

function vehicle(overrides: Partial<Vehicle> & { id: string }): Vehicle {
  return {
    make: "Toyota",
    model: "Hilux",
    year: 2020,
    mileageKm: 50000,
    priceZar: 300000,
    fuelType: "petrol",
    transmission: "manual",
    branch: "Johannesburg",
    ...overrides,
  };
}

function filters(overrides: Partial<Filters> = {}): Filters {
  return { ...DEFAULT_FILTERS, ...overrides };
}

function sort(overrides: Partial<SortState> = {}): SortState {
  return { ...DEFAULT_SORT, ...overrides };
}

const fleet: readonly Vehicle[] = [
  vehicle({ id: "v1", make: "Toyota", model: "Hilux", priceZar: 500000, mileageKm: 60000, year: 2021, fuelType: "diesel" }),
  vehicle({ id: "v2", make: "Toyota", model: "Corolla", priceZar: 300000, mileageKm: 30000, year: 2022, fuelType: "petrol" }),
  vehicle({ id: "v3", make: "BMW", model: "320i", priceZar: 700000, mileageKm: 20000, year: 2020, fuelType: "petrol" }),
  vehicle({ id: "v4", make: "Nissan", model: "Leaf", priceZar: 400000, mileageKm: 10000, year: 2023, fuelType: "electric" }),
  vehicle({ id: "v5", make: "Toyota", model: "Corolla Cross", priceZar: 500000, mileageKm: 40000, year: 2022, fuelType: "hybrid" }),
];

describe("filterVehicles", () => {
  it("returns everything with default filters", () => {
    expect(filterVehicles(fleet, DEFAULT_FILTERS)).toHaveLength(fleet.length);
  });

  it("filters by exact make", () => {
    const result = filterVehicles(fleet, filters({ make: "Toyota" }));
    expect(result.map((v) => v.id)).toEqual(["v1", "v2", "v5"]);
  });

  it("treats an empty fuel selection as all fuel types", () => {
    expect(filterVehicles(fleet, filters({ fuelTypes: [] }))).toHaveLength(5);
  });

  it("matches any of the selected fuel types", () => {
    const result = filterVehicles(fleet, filters({ fuelTypes: ["diesel", "electric"] }));
    expect(result.map((v) => v.id)).toEqual(["v1", "v4"]);
  });

  it("applies inclusive min and max price bounds", () => {
    expect(
      filterVehicles(fleet, filters({ minPrice: 300000, maxPrice: 500000 })).map((v) => v.id),
    ).toEqual(["v1", "v2", "v4", "v5"]);
  });

  it("searches make and model case-insensitively", () => {
    expect(filterVehicles(fleet, filters({ search: "corolla" })).map((v) => v.id)).toEqual([
      "v2",
      "v5",
    ]);
    expect(filterVehicles(fleet, filters({ search: "320I" })).map((v) => v.id)).toEqual(["v3"]);
  });

  it("ignores surrounding whitespace in the search term", () => {
    expect(filterVehicles(fleet, filters({ search: "  leaf  " })).map((v) => v.id)).toEqual(["v4"]);
  });

  it("combines every active filter with AND", () => {
    const result = filterVehicles(
      fleet,
      filters({ make: "Toyota", fuelTypes: ["hybrid"], search: "cross" }),
    );
    expect(result.map((v) => v.id)).toEqual(["v5"]);
  });

  it("returns an empty array when nothing matches", () => {
    expect(filterVehicles(fleet, filters({ make: "Audi" }))).toEqual([]);
  });

  it("does not mutate the input array", () => {
    const snapshot = fleet.map((v) => v.id);
    filterVehicles(fleet, filters({ make: "Toyota" }));
    expect(fleet.map((v) => v.id)).toEqual(snapshot);
  });
});

describe("sortVehicles", () => {
  it("sorts by price ascending", () => {
    expect(sortVehicles(fleet, sort({ key: "price", direction: "asc" })).map((v) => v.id)).toEqual([
      "v2",
      "v4",
      "v1",
      "v5",
      "v3",
    ]);
  });

  it("sorts by price descending", () => {
    expect(sortVehicles(fleet, sort({ key: "price", direction: "desc" })).map((v) => v.id)).toEqual([
      "v3",
      "v1",
      "v5",
      "v4",
      "v2",
    ]);
  });

  it("sorts by mileage ascending", () => {
    expect(sortVehicles(fleet, sort({ key: "mileage", direction: "asc" })).map((v) => v.id)).toEqual([
      "v4",
      "v3",
      "v2",
      "v5",
      "v1",
    ]);
  });

  it("sorts by year descending", () => {
    expect(sortVehicles(fleet, sort({ key: "year", direction: "desc" })).map((v) => v.id)).toEqual([
      "v4",
      "v2",
      "v5",
      "v1",
      "v3",
    ]);
  });

  it("breaks ties by id so ordering is deterministic", () => {
    const tied = [vehicle({ id: "b", priceZar: 100 }), vehicle({ id: "a", priceZar: 100 })];
    expect(sortVehicles(tied, sort({ key: "price", direction: "asc" })).map((v) => v.id)).toEqual([
      "a",
      "b",
    ]);
  });

  it("does not mutate the input array", () => {
    const snapshot = fleet.map((v) => v.id);
    sortVehicles(fleet, sort({ key: "price", direction: "desc" }));
    expect(fleet.map((v) => v.id)).toEqual(snapshot);
  });
});

describe("applyInventoryView", () => {
  it("filters first and then sorts", () => {
    const result = applyInventoryView(
      fleet,
      filters({ make: "Toyota" }),
      sort({ key: "price", direction: "desc" }),
    );
    expect(result.map((v) => v.id)).toEqual(["v1", "v5", "v2"]);
  });
});

describe("getMakes", () => {
  it("returns unique makes in alphabetical order", () => {
    expect(getMakes(fleet)).toEqual(["BMW", "Nissan", "Toyota"]);
  });

  it("returns an empty array for an empty inventory", () => {
    expect(getMakes([])).toEqual([]);
  });
});

describe("toggleFuelType", () => {
  it("adds a fuel type that is not selected", () => {
    expect(toggleFuelType(["petrol"], "diesel")).toEqual(["petrol", "diesel"]);
  });

  it("removes a fuel type that is already selected", () => {
    expect(toggleFuelType(["petrol", "diesel"], "petrol")).toEqual(["diesel"]);
  });
});
