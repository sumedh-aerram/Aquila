.validated == false
and any(.blockers[]; .code == "step_failed" and .subject == "behavior")
and any(.result.steps[]; .kind == "behavior" and .verdict == "differ")
