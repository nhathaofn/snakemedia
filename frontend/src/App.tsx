import { ErrorBoundary } from './components/feedback';
import { AppRouter } from './routes';

function App() {
  return (
    <ErrorBoundary>
      <AppRouter />
    </ErrorBoundary>
  );
}

export default App;
