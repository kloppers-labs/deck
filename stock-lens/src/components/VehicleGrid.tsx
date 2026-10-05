import type { Vehicle } from "../lib/types";
import { VehicleCard } from "./VehicleCard";
import styles from "./VehicleGrid.module.css";

interface VehicleGridProps {
  vehicles: readonly Vehicle[];
}

export function VehicleGrid({ vehicles }: VehicleGridProps) {
  return (
    <ul className={styles.grid}>
      {vehicles.map((vehicle) => (
        <li key={vehicle.id} className={styles.item}>
          <VehicleCard vehicle={vehicle} />
        </li>
      ))}
    </ul>
  );
}
