import type { HubIdentity } from './api'

export type DirectoryListingRequest = {
  type: 'signalforge-directory-listing-request'
  hubId: string
  publicUrl: string
  name: string
  region: string
  publicKey: string
  contact?: string
  software: string
  version: string
}

export type ListingPreflightCheck = {
  id: string
  label: string
  ok: boolean
  detail: string
}

const REGISTER_HUB_BASE = 'https://signalforge.org/register-hub.html'

export function buildDirectoryListingRequest(
  hub: Pick<HubIdentity, 'hubId' | 'publicUrl' | 'name' | 'region' | 'publicKey' | 'contact'>,
  version: string,
): DirectoryListingRequest {
  const request: DirectoryListingRequest = {
    type: 'signalforge-directory-listing-request',
    hubId: hub.hubId,
    publicUrl: hub.publicUrl.trim(),
    name: hub.name.trim(),
    region: hub.region.trim(),
    publicKey: hub.publicKey.trim(),
    software: 'SignalForge Hub',
    version,
  }
  const contact = hub.contact?.trim()
  if (contact) request.contact = contact
  return request
}

export function buildRegisterHubUrl(request: DirectoryListingRequest): string {
  const params = new URLSearchParams({
    hubId: request.hubId,
    publicUrl: request.publicUrl,
    name: request.name,
    region: request.region,
    publicKey: request.publicKey,
    version: request.version,
  })
  if (request.contact) params.set('contact', request.contact)
  return `${REGISTER_HUB_BASE}?${params.toString()}`
}

export function listingPreflightChecks(
  hub: Pick<HubIdentity, 'hubId' | 'publicUrl' | 'name' | 'region' | 'publicKey' | 'contact'>,
  options?: { healthOk?: boolean | null; healthDetail?: string },
): ListingPreflightCheck[] {
  const checks: ListingPreflightCheck[] = []
  const name = hub.name.trim()
  const publicUrl = hub.publicUrl.trim()
  const region = hub.region.trim()
  const publicKey = hub.publicKey.trim()
  const contact = hub.contact?.trim() ?? ''

  checks.push({
    id: 'name',
    label: 'Display name',
    ok: name.length >= 2,
    detail: name ? name : 'Set a display name on this hub.',
  })
  checks.push({
    id: 'publicUrl',
    label: 'Public URL',
    ok: /^https?:\/\/.+/i.test(publicUrl) && !publicUrl.includes('localhost'),
    detail: publicUrl
      ? publicUrl.includes('localhost')
        ? 'Use your public HTTPS URL, not localhost.'
        : publicUrl
      : 'Set the URL operators use to reach this hub.',
  })
  checks.push({
    id: 'region',
    label: 'Region',
    ok: region.length >= 2,
    detail: region ? region : 'Add a city, county, or service area.',
  })
  checks.push({
    id: 'contact',
    label: 'Contact email',
    ok: contact.includes('@'),
    detail: contact ? contact : 'Add a contact email so directory reviewers can reach you.',
  })
  checks.push({
    id: 'publicKey',
    label: 'Ed25519 public key',
    ok: publicKey.startsWith('ed25519:'),
    detail: publicKey ? 'Key ready.' : 'Generate a hub keypair before registering.',
  })
  checks.push({
    id: 'hubId',
    label: 'Hub ID',
    ok: hub.hubId.startsWith('hub_'),
    detail: hub.hubId || 'Hub ID not initialized yet.',
  })

  if (options?.healthOk === true) {
    checks.push({
      id: 'health',
      label: 'Public health check',
      ok: true,
      detail: options.healthDetail ?? 'GET /api/v1/health responded OK.',
    })
  } else if (options?.healthOk === false) {
    checks.push({
      id: 'health',
      label: 'Public health check',
      ok: false,
      detail: options.healthDetail ?? 'Could not reach /api/v1/health at the public URL.',
    })
  } else {
    checks.push({
      id: 'health',
      label: 'Public health check',
      ok: false,
      detail: 'Run CHECK REACHABILITY to verify your public URL.',
    })
  }

  return checks
}

export function allPreflightPassed(checks: ListingPreflightCheck[]): boolean {
  return checks.every((check) => check.ok)
}
