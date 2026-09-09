export function groupConsecutiveTasks(data) {
  const groups = [];
  let currentGroup = [];
  let currentKey = null;

  for (const entry of data) {
    const key = JSON.stringify([entry.State, entry.SpIDs || entry.SpID, entry.Name, entry.OwnerID]);
    if (key !== currentKey) {
      if (currentGroup.length > 0) {
        groups.push(currentGroup);
      }
      currentGroup = [entry];
      currentKey = key;
    } else {
      currentGroup.push(entry);
    }
  }

  if (currentGroup.length > 0) {
    groups.push(currentGroup);
  }

  return groups;
}
