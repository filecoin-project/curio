export function provingIssueReasons(summary) {
  if (!summary) return []

  const reasons = []
  const faulted = summary.faultedPeriods30d ?? 0
  const failed = summary.provingFailureCount ?? 0
  const success = summary.provingSuccessCount ?? 0
  const total = success + failed
  const rate = summary.provingSuccessRate

  if (faulted > 0) {
    reasons.push(`${faulted} dataset${faulted === 1 ? '' : 's'} marked unrecoverable in the last 30 days`)
  }
  if (failed > 0) {
    reasons.push(`${failed} prove task${failed === 1 ? '' : 's'} failed in the last 30 days`)
  }
  if (total > 0 && rate != null && rate < 90) {
    reasons.push(`Proving success rate is ${rate.toFixed(rate >= 99 ? 2 : 1)}% (below 90%)`)
  }
  return reasons
}

export function hasProvingIssues(summary) {
  return provingIssueReasons(summary).length > 0
}

export function walletAsideBadge(keyStatus, { keyStatusLoading = false } = {}) {
  if (keyStatusLoading || keyStatus === undefined) {
    return { label: 'Loading…', tone: 'warn' }
  }
  if (!keyStatus?.configured) {
    return { label: 'No wallet', tone: 'warn' }
  }
  if (!keyStatus.funded) {
    return { label: 'Low balance', tone: 'bad' }
  }
  return null
}
