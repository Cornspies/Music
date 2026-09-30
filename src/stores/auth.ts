"use client"

import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { User } from "@/lib/types"

interface AuthResponse {
  token: string
  user: User
}

async function authPost(
  path: string,
  username: string,
  password: string,
  inviteCode?: string,
): Promise<AuthResponse> {
  let res: Response
  try {
    res = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password, inviteCode }),
    })
  } catch {
    throw new Error("Network error — could not reach the server.")
  }
  let data: { token?: string; user?: User; error?: string } = {}
  try {
    data = (await res.json()) as typeof data
  } catch {}
  if (!res.ok || !data.token || !data.user) {
    throw new Error(data.error || `Request failed (${res.status})`)
  }
  return { token: data.token, user: data.user }
}

interface AuthState {
  token: string | null
  user: User | null
  login: (username: string, password: string) => Promise<void>
  register: (username: string, password: string, inviteCode?: string) => Promise<void>
  logout: () => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      user: null,
      login: async (username, password) => {
        const { token, user } = await authPost("/api/auth/login", username, password)
        set({ token, user })
      },
      register: async (username, password, inviteCode) => {
        const { token, user } = await authPost("/api/auth/register", username, password, inviteCode)
        set({ token, user })
      },
      logout: () => set({ token: null, user: null }),
    }),
    { name: "music-auth" }
  )
)
