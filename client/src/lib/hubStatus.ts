export function trustTextClass(trustLevel = 'community'): string {
  switch (trustLevel) {
    case 'official':
    case 'verified':
      return 'text-console-accent'
    case 'trusted':
      return 'text-console-amber'
    case 'suspended':
      return 'text-console-error'
    default:
      return 'text-console-muted'
  }
}

export function directoryStatusClass(status = 'unverified'): string {
  switch (status) {
    case 'verified':
    case 'listed':
      return 'text-console-accent'
    case 'suspended':
      return 'text-console-error'
    default:
      return 'text-console-muted'
  }
}
