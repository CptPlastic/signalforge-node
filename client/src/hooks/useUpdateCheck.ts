import { useCallback, useEffect, useMemo, useState } from 'react'

import { api, type UpdateCheckResponse, type VersionInfo } from '../lib/api'
import { deploymentFooterLabel, deploymentHeaderLabel, deploymentTitle } from '../lib/format'

function fallbackUpdateInfo(message: string): UpdateCheckResponse {
  return {
    current: {},
    updateAvailable: false,
    checkUrl: '',
    error: message,
  }
}

export function useUpdateCheck() {
  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null)
  const [updateInfo, setUpdateInfo] = useState<UpdateCheckResponse | null>(null)

  const refreshUpdateCheck = useCallback(() =>
    api.updateCheck()
      .then(setUpdateInfo)
      .catch((err) => {
        console.error(err)
        const message = err instanceof Error && err.message ? err.message : 'update check failed'
        setUpdateInfo(fallbackUpdateInfo(message))
      }), [])

  useEffect(() => {
    api.version()
      .then((info) => setVersionInfo(info))
      .catch(() => {
        // keep header usable even if version endpoint is briefly unavailable
      })

    refreshUpdateCheck()
  }, [refreshUpdateCheck])

  const headerVersionLabel = useMemo(() => deploymentHeaderLabel(versionInfo), [versionInfo])
  const headerVersionTitle = useMemo(() => deploymentTitle(versionInfo), [versionInfo])
  const footerDeploymentLabel = useMemo(() => deploymentFooterLabel(versionInfo), [versionInfo])

  const currentDeploymentTag = versionInfo?.deployTag || versionInfo?.commit?.slice(0, 8) || updateInfo?.current?.deployTag || 'unknown'
  const latestUpdateTag = updateInfo?.latest?.imageTag || updateInfo?.latest?.shortCommit || updateInfo?.latest?.commit?.slice(0, 8) || ''
  const clientDetectedUpdate = Boolean(
    updateInfo?.latest &&
    latestUpdateTag &&
    currentDeploymentTag !== 'unknown' &&
    latestUpdateTag.toLowerCase() !== currentDeploymentTag.toLowerCase(),
  )
  const hasUpdateAvailable = Boolean(updateInfo?.updateAvailable || clientDetectedUpdate)

  const updateTitle = useMemo(() => {
    if (!updateInfo?.latest) return updateInfo?.error || 'update check unavailable'
    const latest = updateInfo.latest
    return [
      `latest tag: ${latest.imageTag || latest.shortCommit || 'unknown'}`,
      latest.commit ? `latest commit: ${latest.commit}` : '',
      latest.publishedAt ? `published: ${latest.publishedAt}` : '',
      `current tag: ${currentDeploymentTag}`,
    ].filter(Boolean).join('\n')
  }, [currentDeploymentTag, updateInfo])

  let updateStatusLabel = 'current'
  if (updateInfo?.error) updateStatusLabel = 'check failed'
  if (hasUpdateAvailable) updateStatusLabel = 'update available'
  const updateStatusClass = hasUpdateAvailable ? 'text-console-amber' : 'text-console-accent'

  return {
    versionInfo,
    updateInfo,
    refreshUpdateCheck,
    headerVersionLabel,
    headerVersionTitle,
    footerDeploymentLabel,
    currentDeploymentTag,
    latestUpdateTag,
    hasUpdateAvailable,
    updateTitle,
    updateStatusLabel,
    updateStatusClass,
  }
}