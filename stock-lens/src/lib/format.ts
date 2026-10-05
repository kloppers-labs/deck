const priceFormatter = new Intl.NumberFormat("en-ZA", {
  style: "currency",
  currency: "ZAR",
  maximumFractionDigits: 0,
});

const integerFormatter = new Intl.NumberFormat("en-ZA", {
  maximumFractionDigits: 0,
});

export function formatPrice(priceZar: number): string {
  return priceFormatter.format(priceZar);
}

export function formatMileage(mileageKm: number): string {
  return `${integerFormatter.format(mileageKm)} km`;
}
