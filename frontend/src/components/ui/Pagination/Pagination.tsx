import { Button } from '../Button/Button';
import './Pagination.css';

interface PaginationProps {
  label: string;
  page: number;
  pageCount: number;
  onPageChange: (page: number) => void;
}

export function Pagination({ label, page, pageCount, onPageChange }: PaginationProps) {
  if (pageCount <= 1) return null;
  return (
    <nav className="pagination" aria-label={label}>
      <Button size="sm" disabled={page === 0} onClick={() => onPageChange(page - 1)}>
        Trang trước
      </Button>
      <span aria-live="polite">
        Trang {page + 1} / {pageCount}
      </span>
      <Button size="sm" disabled={page + 1 >= pageCount} onClick={() => onPageChange(page + 1)}>
        Trang sau
      </Button>
    </nav>
  );
}
