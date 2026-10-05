export const FUEL_TYPES = ["petrol", "diesel", "hybrid", "electric"] as const;
export type FuelType = (typeof FUEL_TYPES)[number];

export const TRANSMISSIONS = ["manual", "automatic"] as const;
export type Transmission = (typeof TRANSMISSIONS)[number];

export const BRANCHES = [
  "Johannesburg",
  "Cape Town",
  "Durban",
  "Pretoria",
  "Port Elizabeth",
] as const;
export type Branch = (typeof BRANCHES)[number];

export interface Vehicle {
  readonly id: string;
  readonly make: string;
  readonly model: string;
  readonly year: number;
  readonly mileageKm: number;
  readonly priceZar: number;
  readonly fuelType: FuelType;
  readonly transmission: Transmission;
  readonly branch: Branch;
}

/** Raised when the raw inventory JSON does not match the `Vehicle` shape. */
export class VehicleValidationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "VehicleValidationError";
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function isInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value);
}

function isOneOf<T extends string>(value: unknown, options: readonly T[]): value is T {
  return typeof value === "string" && options.some((option) => option === value);
}

/**
 * Hand-written type guard. Checks the shape of a single vehicle, including
 * membership in the closed unions and basic numeric sanity.
 */
export function isVehicle(value: unknown): value is Vehicle {
  if (!isRecord(value)) {
    return false;
  }

  return (
    isNonEmptyString(value.id) &&
    isNonEmptyString(value.make) &&
    isNonEmptyString(value.model) &&
    isInteger(value.year) &&
    value.year >= 1900 &&
    isInteger(value.mileageKm) &&
    value.mileageKm >= 0 &&
    isInteger(value.priceZar) &&
    value.priceZar >= 0 &&
    isOneOf(value.fuelType, FUEL_TYPES) &&
    isOneOf(value.transmission, TRANSMISSIONS) &&
    isOneOf(value.branch, BRANCHES)
  );
}

/**
 * Validates an unknown JSON payload as a `Vehicle[]`. Throws a
 * `VehicleValidationError` naming the first offending entry.
 */
export function parseVehicles(value: unknown): Vehicle[] {
  if (!Array.isArray(value)) {
    throw new VehicleValidationError(
      "Inventory must be an array of vehicles, but a different value was provided.",
    );
  }

  const vehicles: Vehicle[] = [];
  for (const [index, entry] of value.entries()) {
    if (!isVehicle(entry)) {
      throw new VehicleValidationError(
        `Vehicle at index ${index} is invalid: ${describeInvalidVehicle(entry)}`,
      );
    }
    vehicles.push(entry);
  }

  return vehicles;
}

function describeInvalidVehicle(value: unknown): string {
  if (!isRecord(value)) {
    return `expected an object but received ${Array.isArray(value) ? "an array" : typeof value}.`;
  }

  const missing = [
    "id",
    "make",
    "model",
    "year",
    "mileageKm",
    "priceZar",
    "fuelType",
    "transmission",
    "branch",
  ].filter((key) => !(key in value));

  if (missing.length > 0) {
    return `missing required field(s): ${missing.join(", ")}.`;
  }

  return "one or more fields have an unexpected type or value.";
}
