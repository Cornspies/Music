import { Home } from "./Home"

export function generateStaticParams() {
  return [{ slug: [] }]
}

export default function Page() {
  return <Home />
}
