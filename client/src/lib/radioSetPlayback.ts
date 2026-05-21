import type { Dispatch, SetStateAction } from 'react'
import type { Call, RadioSet } from './api'

type MaybePlayActiveRadioSetCallArgs = Readonly<{
  call: Call
  playCall: (call: Call) => void
  setPlayingId: Dispatch<SetStateAction<number | null>>
  setRadioSets: Dispatch<SetStateAction<RadioSet[]>>
  setRsPlayingID: Dispatch<SetStateAction<string | null>>
}>

export function maybePlayActiveRadioSetCall({
  call,
  playCall,
  setPlayingId,
  setRadioSets,
  setRsPlayingID,
}: MaybePlayActiveRadioSetCallArgs) {
  setRsPlayingID((activeID) => {
    if (activeID) {
      setRadioSets((sets) => {
        const activeSet = sets.find((radioSet) => radioSet.id === activeID)
        if (activeSet?.talkgroups.includes(call.talkgroup)) {
          setPlayingId((currentPlaying) => {
            if (!currentPlaying) {
              playCall(call)
            }
            return currentPlaying
          })
        }
        return sets
      })
    }
    return activeID
  })
}