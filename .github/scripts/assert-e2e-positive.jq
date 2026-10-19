.validated == true
and (.coverage.impacted | index("POST /checkout") != null)
and (.coverage.exercised | index("POST /checkout") != null)
and any(.result.steps[]; .kind == "behavior" and .verdict == "match"
  and any(.steps[]; .method == "POST" and .path == "/checkout"
    and .baseline_status > 0 and .patch_status > 0))
