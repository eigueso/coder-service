import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { getStoredToken, setStoredToken } from '../api/client'
import { AuthContext } from './auth-context'

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setTokenState] = useState<string | null>(() => getStoredToken())

  const setToken = useCallback((next: string | null) => {
    setStoredToken(next)
    setTokenState(next)
  }, [])

  const logout = useCallback(() => {
    setToken(null)
  }, [setToken])

  const value = useMemo(
    () => ({
      token,
      isAuthenticated: Boolean(token),
      setToken,
      logout,
    }),
    [token, setToken, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
