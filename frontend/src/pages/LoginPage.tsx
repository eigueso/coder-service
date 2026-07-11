import { useMutation } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { login } from '../api/client'
import { ApiError } from '../api/types'
import { useAuth } from '../auth/useAuth'
import { ArrowUpRight, Boxes } from 'lucide-react'
import { motion } from 'motion/react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

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
    <main className="grid min-h-screen bg-background lg:grid-cols-[1.15fr_0.85fr]">
      <motion.section
        initial={{ opacity: 0, x: -16 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.35, ease: 'easeOut' }}
        className="hidden bg-zinc-950 p-12 text-white lg:flex lg:flex-col lg:justify-between"
      >
        <div className="flex items-center gap-3 text-sm font-semibold">
          <span className="grid h-9 w-9 place-items-center rounded-lg bg-white text-zinc-950"><Boxes className="h-5 w-5" /></span>
          Coder Service
        </div>
        <div className="max-w-md">
          <p className="text-sm font-medium text-zinc-400">Cloud development environments</p>
          <h1 className="mt-4 text-4xl font-semibold leading-tight tracking-tight">A focused place to build, from any machine.</h1>
          <p className="mt-5 leading-7 text-zinc-400">Launch an environment that is ready for your code, editor, and terminal in minutes.</p>
        </div>
        <p className="text-sm text-zinc-500">Secure workspace access, powered by Coder.</p>
      </motion.section>
      <motion.section
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3, ease: 'easeOut', delay: 0.1 }}
        className="mx-auto flex w-full max-w-md flex-col justify-center px-6 py-12"
      >
        <div className="mb-8 lg:hidden"><span className="grid h-10 w-10 place-items-center rounded-lg bg-primary text-primary-foreground"><Boxes className="h-5 w-5" /></span></div>
        <div className="mb-7">
          <h1 className="text-3xl font-semibold tracking-tight text-foreground">Welcome back</h1>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">Sign in with your Coder email to access your workspaces.</p>
        </div>
        <Card className="shadow-sm">
          <CardContent>
            <form onSubmit={onSubmit}>
              <Label className="block">
                Email address
                <Input type="email" autoComplete="username" required value={email} onChange={(event) => setEmail(event.target.value)} placeholder="you@company.com" className="mt-2 h-10" />
              </Label>
              {errorMessage ? <Alert variant="destructive" className="mt-4"><AlertDescription>{errorMessage}</AlertDescription></Alert> : null}
              <Button type="submit" disabled={mutation.isPending} className="mt-6 h-10 w-full">
                {mutation.isPending ? 'Signing in…' : <>Continue <ArrowUpRight className="h-4 w-4" /></>}
              </Button>
            </form>
          </CardContent>
        </Card>
      <p className="mt-5 text-center text-xs leading-5 text-muted-foreground">Your email is used to securely connect you to Coder. No password is required.</p>
      </motion.section>
    </main>
  )
}
