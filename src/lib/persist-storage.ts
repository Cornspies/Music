import { createJSONStorage, type StateStorage } from "zustand/middleware"

export function changeOnlyStorage<S>() {
  return createJSONStorage<S>(() => {
    const target = window.localStorage
    const written = new Map<string, string>()
    const storage: StateStorage = {
      getItem: (name) => target.getItem(name),
      setItem: (name, value) => {
        if (written.get(name) === value) return
        written.set(name, value)
        target.setItem(name, value)
      },
      removeItem: (name) => {
        written.delete(name)
        target.removeItem(name)
      },
    }
    return storage
  })
}
