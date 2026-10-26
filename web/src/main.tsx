import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles/base.css'
import './styles/app.css'
import './styles/notes.css'
import './styles/settings.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import Login from './pages/Login'
import Notes from './pages/Notes'
import Tasks from './pages/Tasks'
import Today from './pages/Today'
import Calendar from './pages/Calendar'
import Trash from './pages/Trash'
import Settings from './pages/Settings'
import NewFrom from './pages/NewFrom'
import Share from './pages/Share'
import EmbedNote, { EmbedClosed, EmbedGuard } from './pages/EmbedNote'
import { ErrorBoundary } from './components/ErrorBoundary'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

function App() {
  return (<>
    <EmbedGuard />
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route path="/s/:token" element={<Share />} />
      <Route path="/settings/:tab?" element={<Gate><ErrorBoundary where="settings"><Settings /></ErrorBoundary></Gate>} />
      <Route path="/embed/note/:id" element={<Gate><ErrorBoundary where="note"><EmbedNote /></ErrorBoundary></Gate>} />
      <Route path="/embed/closed" element={<EmbedClosed />} />
      <Route element={<Gate><Layout /></Gate>}>
        <Route path="/" element={<Notes />} />
        <Route path="/note/:id" element={<Notes />} />
        <Route path="/today" element={<Today />} />
        <Route path="/tasks" element={<Tasks />} />
        <Route path="/calendar" element={<Calendar />} />
        <Route path="/trash" element={<Trash />} />
        <Route path="/new" element={<NewFrom />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  </>)
}

function Root() {
  // The public share page needs no account: it does not wait for one.
  if (location.pathname.startsWith('/s/')) return <ToastProvider><Routes><Route path="/s/:token" element={<Share />} /></Routes></ToastProvider>
  return <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <Root />
    </BrowserRouter>
  </React.StrictMode>,
)
