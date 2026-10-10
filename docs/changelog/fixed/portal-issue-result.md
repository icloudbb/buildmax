- A Portal Issue now answers "did its run work, and what did it produce" once:
  **Latest run** shows the newest Agent or Workflow run, its status, the files
  it published and its reply or result, and a link to it; Results shows text
  output instead of claiming nothing was produced; and Runs is one list with one
  count. The Issue flow API returns `runs` as that one list in place of
  `agent_tasks` and `latest_result`. A Workflow run without a declared result
  says so and points to its steps, whose output is labelled **Output**.
