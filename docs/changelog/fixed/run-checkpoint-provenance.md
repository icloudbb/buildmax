- A task run read now includes its workspace checkpoint ids
  (`workspace_base/result/partial_checkpoint_id`); as a result a committed
  result checkpoint again records the base it was built from, which had gone
  unrecorded because the base never reached the finalizer.
