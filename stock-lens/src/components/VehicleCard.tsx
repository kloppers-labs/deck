import { formatMileage, formatPrice } from "../lib/format";
import type { Vehicle } from "../lib/types";
import styles from "./VehicleCard.module.css";

interface VehicleCardProps {
  vehicle: Vehicle;
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

export function VehicleCard({ vehicle }: VehicleCardProps) {
  const title = `${vehicle.year} ${vehicle.make} ${vehicle.model}`;
  const price = formatPrice(vehicle.priceZar);
  const mileage = formatMileage(vehicle.mileageKm);
  const label = `${title}. Price ${price}. ${mileage}. ${capitalize(
    vehicle.fuelType,
  )}, ${vehicle.transmission}. ${vehicle.branch} branch.`;

  return (
    <article className={styles.card} tabIndex={0} aria-label={label}>
      <header className={styles.header}>
        <h3 className={styles.title}>{title}</h3>
        <p className={styles.price}>{price}</p>
      </header>

      <dl className={styles.specs}>
        <div className={styles.spec}>
          <dt>Mileage</dt>
          <dd>{mileage}</dd>
        </div>
        <div className={styles.spec}>
          <dt>Fuel</dt>
          <dd>{capitalize(vehicle.fuelType)}</dd>
        </div>
        <div className={styles.spec}>
          <dt>Transmission</dt>
          <dd>{capitalize(vehicle.transmission)}</dd>
        </div>
      </dl>

      <p className={styles.branch}>{vehicle.branch}</p>
    </article>
  );
}
