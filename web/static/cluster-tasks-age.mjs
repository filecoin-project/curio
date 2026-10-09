import {interpolateClusterTaskAgeSeconds as interpolate} from './cluster-tasks-model.mjs';

export function clusterTaskAge(entry, clock) {
  const pending = entry.OwnerID === null || entry.OwnerID === undefined;
  if (pending) {
    const age = interpolate(entry.AgeSeconds, clock);
    return {seconds: age, text: age === null ? 'unknown' : null, title: 'Waiting since posting; this task has not started.'};
  }
  const age = entry.TookState === 'running' ? interpolate(entry.TookSeconds, clock) : null;
  if (age !== null) return {seconds:age, text:null, title:'Took since entry into the current task Do attempt; the same start is used by new History records.'};
  if (entry.TookState === 'awaiting-start') return {seconds:null, text:'—', title:'No current attempt execution start has been confirmed; ownership alone is not execution.'};
  return {seconds:null, text:'unknown', title:entry.TookState === 'future-start' ? 'The recorded start is ahead of the server snapshot; check clock synchronization.' : 'The current attempt start is unknown. Posted, claim and migration times are not used as Took.'};
}
