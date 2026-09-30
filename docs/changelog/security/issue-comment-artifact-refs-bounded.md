- A task run's issue comment now bounds the number and length of the Artifact
  references it records, so model-chosen worker code cannot write an unbounded
  or oversized list into the comment body.
