import { useMutation } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { login } from '../api/client'
import { ApiError } from '../api/types'
import { useAuth } from '../auth/AuthContext'

export function LoginPage() {
  const { isAuthenticated, setToken } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as { from?: string } | null)?.from ?? '/workspaces'

  const [email, setEmail] = useState('')

  const mutation = useMutation({
    mutationFn: () => login(email.trim()),
    onSuccess: (data) => {
      setToken(data.session_token)
      navigate(from, { replace: true })
    },
  })

  if (isAuthenticated) {
    return <Navigate to="/workspaces" replace />
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault()
    mutation.mutate()
  }

  const errorMessage =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.error
        ? 'Unable to sign in. Try again.'
        : null

  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col justify-center px-6 py-16">
      <div className="mb-10">
        <p className="font-mono text-xs tracking-[0.2em] text-accent uppercase">coder-service</p>
        <h1 className="mt-3 text-3xl font-semibold tracking-tight text-ink">Sign in</h1>
        <p className="mt-2 text-sm leading-relaxed text-ink-muted">
          Enter your Coder user email. The backend mints a token for that user using the
          configured owner session token (SSO simulation — no password).
        </p>
      </div>

      <form
        onSubmit={onSubmit}
        className="rounded-2xl border border-line bg-panel/90 p-6 shadow-[0_20px_60px_-40px_rgba(15,28,26,0.45)] backdrop-blur"
      >
        <label className="block text-sm font-medium text-ink">
          Email
          <input
            type="email"
            autoComplete="username"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="mt-2 w-full rounded-xl border border-line bg-surface px-3 py-2.5 outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
          />
        </label>

        {errorMessage ? (
          <p className="mt-4 rounded-xl bg-danger-soft px-3 py-2 text-sm text-danger" role="alert">
            {errorMessage}
          </p>
        ) : null}

        <button
          type="submit"
          disabled={mutation.isPending}
          className="mt-6 w-full rounded-xl bg-accent px-4 py-2.5 text-sm font-semibold text-white transition hover:bg-accent-strong disabled:cursor-not-allowed disabled:opacity-60"
        >
          {mutation.isPending ? 'Signing in…' : 'Continue'}
        </button>
      </form>
    </main>
  )
}
