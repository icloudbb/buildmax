- A Kubernetes worker whose run failed and reported FAILED now exits zero, so
  its Job no longer restarts a pod that only refuses the run as already
  claimed and hides the first container's log behind that restart.
