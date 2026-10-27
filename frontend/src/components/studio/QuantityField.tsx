import './Studio.css';

/** Output count picker (Đơn only), x1 to the provider's per-request maximum. */
export function QuantityField({
  value,
  max,
  onChange,
  ariaLabel,
}: {
  value: number;
  max: number;
  onChange: (value: number) => void;
  ariaLabel: string;
}) {
  return (
    <div className="studio-field-group">
      <label className="studio-field-label">
        <span>Số lượng</span>
      </label>
      <div className="quantity-grid" role="group" aria-label={ariaLabel}>
        {Array.from({ length: max }, (_, i) => i + 1).map((num) => (
          <button
            key={num}
            type="button"
            className={`quantity-btn ${value === num ? 'active' : ''}`}
            onClick={() => onChange(num)}
            aria-pressed={value === num}
          >
            x{num}
          </button>
        ))}
      </div>
    </div>
  );
}
