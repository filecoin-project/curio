use std::fs;
use std::io;
use std::path::Path;

pub fn set_oom_score_adj(pid: i32, score: i16) -> io::Result<()> {
    let path = format!("/proc/{}/oom_score_adj", pid);
    fs::write(Path::new(&path), score.to_string())
} 