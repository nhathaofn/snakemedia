import { ErrorBoundary } from './components/feedback';
import { ConfirmDialogHost } from './components/ui';
import { AppRouter } from './routes';

function App() {
  return (
    <ErrorBoundary>
      <AppRouter />
      <ConfirmDialogHost />
    </ErrorBoundary>
  );
}

export default App;
