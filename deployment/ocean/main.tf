data "digitalocean_project" "persistent" {
  name = var.project_name
}

data "digitalocean_vpc" "persistent" {
  name = var.vpc_name
}

data "digitalocean_spaces_bucket" "persistent" {
  name   = var.spaces_bucket_name
  region = var.region
}

data "digitalocean_kubernetes_versions" "beta" {
  version_prefix = var.kubernetes_version_prefix
}

resource "digitalocean_kubernetes_cluster" "beta" {
  name    = var.kubernetes_cluster_name
  region  = var.region
  version = data.digitalocean_kubernetes_versions.beta.latest_version

  vpc_uuid                         = data.digitalocean_vpc.persistent.id
  ha                               = false
  auto_upgrade                     = false
  surge_upgrade                    = false
  destroy_all_associated_resources = true

  node_pool {
    name       = "system"
    size       = var.kubernetes_node_size
    node_count = var.kubernetes_node_count
  }

  lifecycle {
    precondition {
      condition     = data.digitalocean_vpc.persistent.region == var.region
      error_message = "The existing VPC must be in the configured region."
    }
  }
}

resource "digitalocean_database_cluster" "beta" {
  name                 = var.database_cluster_name
  engine               = "mysql"
  version              = var.database_version
  size                 = var.database_size
  region               = var.region
  node_count           = 1
  private_network_uuid = data.digitalocean_vpc.persistent.id
  project_id           = data.digitalocean_project.persistent.id

  lifecycle {
    precondition {
      condition     = data.digitalocean_vpc.persistent.region == var.region
      error_message = "The existing VPC must be in the configured region."
    }
  }
}

resource "digitalocean_database_db" "buildmax" {
  cluster_id = digitalocean_database_cluster.beta.id
  name       = var.database_name
}

data "digitalocean_database_ca" "beta" {
  cluster_id = digitalocean_database_cluster.beta.id
}

resource "digitalocean_database_firewall" "beta" {
  cluster_id = digitalocean_database_cluster.beta.id

  rule {
    type  = "k8s"
    value = digitalocean_kubernetes_cluster.beta.id
  }
}

resource "digitalocean_project_resources" "kubernetes" {
  project   = data.digitalocean_project.persistent.id
  resources = [digitalocean_kubernetes_cluster.beta.urn]
}
