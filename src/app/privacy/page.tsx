import type { Metadata } from "next"
import { PrivacyPolicy } from "./PrivacyPolicy"

export const metadata: Metadata = {
  title: "Privacy policy · Music",
  description: "What data Music handles, why, who can see it, and what you can do about it.",
}

export default function PrivacyPage() {
  return <PrivacyPolicy />
}
