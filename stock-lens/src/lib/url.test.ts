import { describe, expect, it } from "vitest";
import { DEFAULT_FILTERS, DEFAULT_SORT } from "./inventory";
import type { InventoryState } from "./url";
import {
  parseInventoryState,
  serializeInventoryState,
  toShareableUrl,
} from "./url";

const defaultState: InventoryState = {
  filters: DEFAULT_FILTERS,
  sort: DEFAULT_SORT,
};

describe("parseInventoryState", () => {
  it("returns defaults for an empty query string", () => {
    expect(parseInventoryState("")).toEqual(defaultState);
  });

  it("parses every supported parameter", () => {
    const state = parseInventoryState(
      "?make=Toyota&fuel=diesel,hybrid&min=200000&max=600000&q=hilux&sort=mileage&dir=desc",
    );
    expect(state.filters.make).toBe("Toyota");
    expect(state.filters.fuelTypes).toEqual(["diesel", "hybrid"]);
    expect(state.filters.minPrice).toBe(200000);
    expect(state.filters.maxPrice).toBe(600000);
    expect(state.filters.search).toBe("hilux");
    expect(state.sort).toEqual({ key: "mileage", direction: "desc" });
  });

  it("accepts a query string without the leading question mark", () => {
    expect(parseInventoryState("make=BMW").filters.make).toBe("BMW");
  });

  it("drops unknown fuel types and de-duplicates the rest", () => {
    expect(parseInventoryState("?fuel=diesel,steam,diesel").filters.fuelTypes).toEqual(["diesel"]);
  });

  it("falls back to defaults for invalid numbers", () => {
    const state = parseInventoryState("?min=-5&max=abc");
    expect(state.filters.minPrice).toBeNull();
    expect(state.filters.maxPrice).toBeNull();
  });

  it("falls back to defaults for an invalid sort key or direction", () => {
    const state = parseInventoryState("?sort=colour&dir=sideways");
    expect(state.sort).toEqual(DEFAULT_SORT);
  });
});

describe("serializeInventoryState", () => {
  it("omits default values to keep links short", () => {
    expect(serializeInventoryState(defaultState)).toBe("");
  });

  it("serialises active filters and a non-default sort", () => {
    const query = serializeInventoryState({
      filters: {
        make: "Toyota",
        fuelTypes: ["hybrid", "electric"],
        minPrice: 150000,
        maxPrice: 500000,
        search: "cross",
      },
      sort: { key: "year", direction: "desc" },
    });

    expect(query).toBe(
      "make=Toyota&fuel=hybrid%2Celectric&min=150000&max=500000&q=cross&sort=year&dir=desc",
    );
  });

  it("trims the search term before serialising", () => {
    expect(
      serializeInventoryState({
        filters: { ...DEFAULT_FILTERS, search: "  hilux  " },
        sort: DEFAULT_SORT,
      }),
    ).toBe("q=hilux");
  });
});

describe("round-trip", () => {
  it("restores the exact state it serialised", () => {
    const state: InventoryState = {
      filters: {
        make: "BMW",
        fuelTypes: ["diesel"],
        minPrice: 300000,
        maxPrice: 900000,
        search: "xDrive",
      },
      sort: { key: "mileage", direction: "desc" },
    };

    expect(parseInventoryState(serializeInventoryState(state))).toEqual(state);
  });
});

describe("toShareableUrl", () => {
  it("returns the bare path when the state is default", () => {
    expect(toShareableUrl("/", defaultState)).toBe("/");
  });

  it("appends the serialised query to the path", () => {
    expect(
      toShareableUrl("/stock", {
        filters: { ...DEFAULT_FILTERS, make: "Kia" },
        sort: DEFAULT_SORT,
      }),
    ).toBe("/stock?make=Kia");
  });
});
