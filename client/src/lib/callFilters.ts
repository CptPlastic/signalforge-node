import type { Call, TalkgroupSetting } from './api'

type CallSortBy = 'datetime' | 'duration' | 'frequency' | 'talkgroup'
type CallSortOrder = 'asc' | 'desc'

type BuildFilteredCallsArgs = Readonly<{
  calls: Call[]
  groupFilter: string
  hideMuted: boolean
  search: string
  serverResults: Call[] | null
  settingsMap: Record<number, TalkgroupSetting>
  showFavoritesOnly: boolean
  sortBy: CallSortBy
  sortOrder: CallSortOrder
}>

export function buildFilteredCalls({
  calls,
  groupFilter,
  hideMuted,
  search,
  serverResults,
  settingsMap,
  showFavoritesOnly,
  sortBy,
  sortOrder,
}: BuildFilteredCallsArgs): Call[] {
  const usingLiveStream = serverResults === null
  let list = [...(serverResults ?? calls)]
  const query = search.trim().toLowerCase()

  if (usingLiveStream && query) {
    list = list.filter((call) =>
      [
        String(call.talkgroup),
        call.talkgroupLabel,
        call.talkgroupGroup,
        call.systemLabel,
        call.talkgroupTag,
        call.transcriptText,
      ]
        .join(' ')
        .toLowerCase()
        .includes(query),
    )
  }

  if (usingLiveStream && groupFilter) {
    const groupQuery = groupFilter.toLowerCase()
    list = list.filter((call) => call.talkgroupGroup.toLowerCase().includes(groupQuery))
  }

  if (hideMuted) {
    list = list.filter((call) => !settingsMap[call.talkgroup]?.muted)
  }

  if (usingLiveStream && showFavoritesOnly) {
    list = list.filter((call) => settingsMap[call.talkgroup]?.favorite)
  }

  list.sort((first, second) => {
    const direction = sortOrder === 'asc' ? 1 : -1
    if (sortBy === 'datetime') return (first.dateTime - second.dateTime) * direction
    if (sortBy === 'duration') return (first.duration - second.duration) * direction
    if (sortBy === 'frequency') return (first.frequency - second.frequency) * direction
    return (first.talkgroup - second.talkgroup) * direction
  })

  return list
}

export function formatCallLogCount(serverLoading: boolean, serverResults: Call[] | null, filteredCount: number, totalCount: number): string {
  if (serverLoading) return 'searching...'
  if (serverResults) return `${filteredCount} results`
  return `${filteredCount}/${totalCount} calls`
}

export function formatSavedCallCount(count: number): string {
  return count === 1 ? '1 call saved' : `${count} calls saved`
}