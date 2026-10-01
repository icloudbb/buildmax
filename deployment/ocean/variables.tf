variable "project_name" {
  description = "Existing DigitalOcean Project reused by the qualification environment."
  type        = string
  default     = "buildmax-beta"
}

variable "vpc_name" {
  description = "Existing DigitalOcean VPC reused by the qualification environment."
  type        = string
  default     = "buildmax-beta"
}

variable "spaces_bucket_name" {
  description = "Existing DigitalOcean Spaces bucket reused by BuildMax."
  type        = string
  default     = "buildmax-beta"
}

variable "region" {
  description = "One region shared by the VPC, Spaces bucket, DOKS, and MySQL."
  type        = string
  default     = "sgp1"
}

variable "kubernetes_cluster_name" {
  description = "Name of the disposable DOKS cluster."
  type        = string
  default     = "buildmax-beta-doks"
}

variable "kubernetes_version_prefix" {
  description = "DOKS minor version to run; the newest patch slug under it is selected. A concrete slug, not \"latest\", which DOKS rejects on any later in-place update."
  type        = string
  default     = "1.36."
}

variable "kubernetes_node_size" {
  description = "Size slug for the single DOKS worker node."
  type        = string
  default     = "s-2vcpu-4gb"
}

variable "kubernetes_node_count" {
  description = "Worker nodes in the DOKS pool. Two let the two Server replicas sit on different nodes and leave room for concurrent worker Jobs."
  type        = number
  default     = 2
}

variable "database_cluster_name" {
  description = "Name of the disposable managed MySQL cluster."
  type        = string
  default     = "buildmax-beta-mysql"
}

variable "database_size" {
  description = "Size slug for the single managed MySQL node."
  type        = string
  default     = "db-s-1vcpu-1gb"
}

variable "database_version" {
  description = "DigitalOcean Managed MySQL version. Keep this explicit so qualification infrastructure is reproducible."
  type        = string
  default     = "8.4"
}

variable "database_name" {
  description = "Application database created in the managed MySQL cluster."
  type        = string
  default     = "buildmax"
}
