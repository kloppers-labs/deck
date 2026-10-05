import rawVehicles from "../data/vehicles.json";
import { parseVehicles } from "./types";
import type { Vehicle } from "./types";

export interface VehicleLoadResult {
  readonly vehicles: Vehicle[];
  readonly error: string | null;
}

/**
 * Validates an unknown payload and converts it into a load result. This never
 * throws so the UI can render an error state instead of crashing.
 */
export function loadVehicles(source: unknown): VehicleLoadResult {
  try {
    return { vehicles: parseVehicles(source), error: null };
  } catch (error) {
    return {
      vehicles: [],
      error:
        error instanceof Error
          ? error.message
          : "The inventory could not be validated.",
    };
  }
}

/** The validated inventory, loaded once at module evaluation time. */
export const inventory: VehicleLoadResult = loadVehicles(rawVehicles);
