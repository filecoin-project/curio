use crate::utils::oom::set_oom_score_adj;

pub fn spawn_task(task: Task) -> Result<Child, Error> {
    let child = Command::new(task.program)
        .args(&task.args)
        // ... other command setup ...
        .spawn()?;

    // Set high OOM score for non-PoSt tasks
    if !task.is_post_task() {
        if let Err(e) = set_oom_score_adj(child.id() as i32, 900) {
            log::warn!("Failed to set OOM score adjustment: {}", e);
            // Continue execution even if setting OOM score fails
        }
    }

    Ok(child)
} 