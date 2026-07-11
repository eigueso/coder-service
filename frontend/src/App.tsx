import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import { RequireAuth } from './auth/RequireAuth'
import { LoginPage } from './pages/LoginPage'
import { TerminalPage } from './pages/TerminalPage'
import { WorkspacesPage } from './pages/WorkspacesPage'

export default function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route
            path="/workspaces"
            element={
              <RequireAuth>
                <WorkspacesPage />
              </RequireAuth>
            }
          />
          <Route path="/workspaces/:name/terminal" element={<TerminalPage />} />
          <Route path="/" element={<Navigate to="/workspaces" replace />} />
          <Route path="*" element={<Navigate to="/workspaces" replace />} />
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  )
}
