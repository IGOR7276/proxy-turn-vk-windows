import { Outlet } from 'react-router-dom';
import BottomNav from './BottomNav';

export default function Layout() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', minHeight: '100vh', background: 'var(--bg)', boxSizing: 'border-box' }}>
      <main style={{ flex: 1, padding: '20px 20px 90px', maxWidth: 720, width: '100%', margin: '0 auto', boxSizing: 'border-box' }}>
        <Outlet />
      </main>
      <BottomNav />
    </div>
  );
}

