"use client"

import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { User } from "@/lib/types"
import { SERVER_REFUSES_APP, apiUrl, isDesktop, publicUrl, serverAnswers, serverUrl } from "@/lib/desktop"
import { serverDownFrom, serverDownMessage } from "@/stores/server-status"
import { useOfflineStore } from "@/stores/offline"

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
    res = await fetch(apiUrl(path), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password, inviteCode }),
    })
  } catch {
    if (isDesktop && (await serverAnswers(serverUrl()))) throw new Error(SERVER_REFUSES_APP)
    throw new Error("Network error — could not reach the server.")
  }
  let data: { token?: string; user?: User; error?: string } = {}
  try {
    data = (await res.json()) as typeof data
  } catch {}
  if (!res.ok || !data.token || !data.user) {
    const down = serverDownFrom(res.status, data)
    if (down) throw new Error(serverDownMessage(down))
    throw new Error(data.error || `Request failed (${res.status})`)
  }
  return { token: data.token, user: data.user }
}

interface AuthState {
  token: string | null
  user: User | null
  // When the avatar was last changed on this device. It goes into the avatar's
  // address, which otherwise stays the same and keeps showing the old picture.
  avatarVersion: number
  // The account, on which server, whose downloads and play settings are on
  // this device. It outlives a session that ran out, so the same account finds
  // them again and a different one never gets them.
  owner: string | null
  login: (username: string, password: string) => Promise<void>
  register: (username: string, password: string, inviteCode?: string) => Promise<void>
  signIn: (token: string, user: User) => Promise<void>
  // The session ended by itself: it ran out or was revoked.
  logout: () => void
  // The person signs out: nothing of the account stays on this device.
  signOut: () => Promise<void>
}

function ownerOf(user: User): string {
  return `${publicUrl("")} ${user.id}`
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      token: null,
      user: null,
      avatarVersion: 0,
      owner: null,
      login: async (username, password) => {
        const { token, user } = await authPost("/api/auth/login", username, password)
        await get().signIn(token, user)
      },
      register: async (username, password, inviteCode) => {
        const { token, user } = await authPost("/api/auth/register", username, password, inviteCode)
        await get().signIn(token, user)
      },
      signIn: async (token, user) => {
        const owner = ownerOf(user)
        const last = get().owner
        // Gone before the new account is signed in, so it never sees them.
        if (last && last !== owner) await useOfflineStore.getState().removeAll()
        set({ token, user, owner })
      },
      logout: () => set({ token: null, user: null, avatarVersion: 0 }),
      signOut: async () => {
        await useOfflineStore.getState().removeAll()
        set({ token: null, user: null, avatarVersion: 0, owner: null })
      },
    }),
    {
      name: "music-auth",
      // An account that was signed in before owners were recorded keeps what is on the device.
      merge: (persisted, current) => {
        const saved = (persisted ?? {}) as Partial<AuthState>
        return { ...current, ...saved, owner: saved.owner ?? (saved.user ? ownerOf(saved.user) : null) }
      },
    }
  )
)
