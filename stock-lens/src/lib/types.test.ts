import { describe, expect, it } from "vitest";
import { isVehicle, parseVehicles, VehicleValidationError } from "./types";
import type { Vehicle } from "./types";

const validVehicle: Vehicle = {
  id: "v001",
  make: "Toyota",
  model: "Hilux 2.8 GD-6 Legend",
  year: 2022,
  mileageKm: 48000,
  priceZar: 649900,
  fuelType: "diesel",
  transmission: "automatic",
  branch: "Johannesburg",
};

describe("isVehicle", () => {
  it("accepts a well-formed vehicle", () => {
    expect(isVehicle(validVehicle)).toBe(true);
  });

  it("rejects non-object values", () => {
    expect(isVehicle(null)).toBe(false);
    expect(isVehicle(undefined)).toBe(false);
    expect(isVehicle("Toyota")).toBe(false);
    expect(isVehicle(42)).toBe(false);
    expect(isVehicle([validVehicle])).toBe(false);
  });

  it("rejects an object with a missing field", () => {
    const missing = { ...validVehicle } as Record<string, unknown>;
    delete missing.priceZar;
    expect(isVehicle(missing)).toBe(false);
  });

  it("rejects a value outside the fuel type union", () => {
    expect(isVehicle({ ...validVehicle, fuelType: "steam" })).toBe(false);
  });

  it("rejects a value outside the transmission union", () => {
    expect(isVehicle({ ...validVehicle, transmission: "cvt" })).toBe(false);
  });

  it("rejects a branch that is not one of the five cities", () => {
    expect(isVehicle({ ...validVehicle, branch: "Bloemfontein" })).toBe(false);
  });

  it("rejects blank strings", () => {
    expect(isVehicle({ ...validVehicle, make: "   " })).toBe(false);
  });

  it("rejects non-integer and negative numbers", () => {
    expect(isVehicle({ ...validVehicle, year: 2022.5 })).toBe(false);
    expect(isVehicle({ ...validVehicle, mileageKm: -1 })).toBe(false);
    expect(isVehicle({ ...validVehicle, priceZar: -100 })).toBe(false);
    expect(isVehicle({ ...validVehicle, mileageKm: "48000" })).toBe(false);
  });
});

describe("parseVehicles", () => {
  it("returns a typed array for valid input", () => {
    const parsed = parseVehicles([validVehicle]);
    expect(parsed).toHaveLength(1);
    expect(parsed.map((vehicle) => vehicle.id)).toEqual(["v001"]);
  });

  it("accepts an empty inventory", () => {
    expect(parseVehicles([])).toEqual([]);
  });

  it("throws a VehicleValidationError when the payload is not an array", () => {
    expect(() => parseVehicles({ vehicles: [] })).toThrow(VehicleValidationError);
  });

  it("names the first invalid index in the error message", () => {
    expect(() =>
      parseVehicles([validVehicle, { ...validVehicle, id: "v002", year: 1800 }]),
    ).toThrow(/index 1/);
  });
});
