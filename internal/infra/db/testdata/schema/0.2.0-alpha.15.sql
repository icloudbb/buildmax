-- BuildMax upgrade fixture: what ghcr.io/icloudbb/buildmax:0.2.0-alpha.15 left in MySQL after
-- `./make release upgrade-fixture 0.2.0-alpha.15` seeded it through that release's API.
-- Generated; regenerate rather than edit. TestUpgradeFromPredecessorSchema
-- upgrades it with the candidate and asserts every manifest entity survives.
-- manifest: {"source_image":"ghcr.io/icloudbb/buildmax:0.2.0-alpha.15","owner_id":"yntztn5krglcq4hcnjtq","owner_email":"upgrade-owner@buildmax.local","space_id":"eamqmi4tcsyeuy453pla","agent_id":"na4tx2mzqugn5pphagsq","issue_id":"l5tc4ylqtszmt44kmcoq","workflow_id":"5is6rbrm36itdiwc2fda","workflow_run_id":"orxn4oxxqk5ut5i3cz6a","fired_schedule_id":"r452dfe74cb5tong4lja","fired_task_id":"lb37ups4s63epke65vnq","idle_schedule_id":"berul43d2227hhg5q4zq","artifact_id":"tfrqafpxhgkxhgfaafma","artifact_sha256":"9723d085163fa0d8270120fa785f99cd3f4c6f033b1bb8c6b4ebe690476be515"}
/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!50503 SET NAMES utf8mb4 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;
DROP TABLE IF EXISTS `agent`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `agent` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `space_id` bigint unsigned DEFAULT NULL,
  `name` varchar(255) NOT NULL,
  `description` text,
  `instructions` text,
  `model` varchar(255) DEFAULT NULL,
  `plugins` text,
  `sandbox_network_tier` varchar(64) DEFAULT NULL,
  `sandbox_filesystem_tier` varchar(64) DEFAULT NULL,
  `secret_consumption` text,
  `revision` bigint NOT NULL DEFAULT '1',
  `deleted_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_agent_public_id` (`public_id`),
  KEY `idx_agent_user_id` (`user_id`),
  KEY `idx_agent_space_id` (`space_id`),
  KEY `idx_agent_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `agent` DISABLE KEYS */;
INSERT INTO `agent` VALUES (1,'na4tx2mzqugn5pphagsq',1,2,'Upgrade fixture agent','Seeded by the upgrade fixture.','Summarize the issue.','','','','','',1,NULL,'2026-09-26 17:10:18.238117');
/*!40000 ALTER TABLE `agent` ENABLE KEYS */;
DROP TABLE IF EXISTS `agent_revision`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `agent_revision` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `agent_id` bigint unsigned NOT NULL,
  `revision` bigint NOT NULL,
  `name` varchar(255) NOT NULL,
  `description` text,
  `instructions` text,
  `model` varchar(255) DEFAULT NULL,
  `plugins` text,
  `sandbox_network_tier` varchar(64) DEFAULT NULL,
  `sandbox_filesystem_tier` varchar(64) DEFAULT NULL,
  `secret_consumption` text,
  `created_by` bigint unsigned NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_agent_revision` (`agent_id`,`revision`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `agent_revision` DISABLE KEYS */;
INSERT INTO `agent_revision` VALUES (1,1,1,'Upgrade fixture agent','Seeded by the upgrade fixture.','Summarize the issue.','','','','','',1,'2026-09-26 17:10:18.238603');
/*!40000 ALTER TABLE `agent_revision` ENABLE KEYS */;
DROP TABLE IF EXISTS `artifact`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `artifact` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `space_id` bigint unsigned NOT NULL,
  `filename` varchar(512) NOT NULL,
  `media_type` varchar(255) DEFAULT NULL,
  `size_bytes` bigint NOT NULL,
  `sha256` varchar(64) NOT NULL,
  `storage_key` varchar(1024) NOT NULL,
  `created_by_type` varchar(32) NOT NULL,
  `created_by_id` varchar(64) DEFAULT NULL,
  `source_type` varchar(32) NOT NULL,
  `source_id` varchar(64) DEFAULT NULL,
  `title` varchar(255) DEFAULT NULL,
  `deleted_at` datetime(6) DEFAULT NULL,
  `expires_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_artifact_public_id` (`public_id`),
  KEY `idx_artifact_space_created` (`space_id`,`created_at`),
  KEY `idx_artifact_source_id` (`source_id`),
  KEY `idx_artifact_deleted_at` (`deleted_at`),
  KEY `idx_artifact_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `artifact` DISABLE KEYS */;
INSERT INTO `artifact` VALUES (1,'tfrqafpxhgkxhgfaafma',2,'upgrade-fixture.txt','text/plain; charset=utf-8',26,'9723d085163fa0d8270120fa785f99cd3f4c6f033b1bb8c6b4ebe690476be515','/data/workspaces/spaces/eamqmi4tcsyeuy453pla/artifacts/tfrqafpxhgkxhgfaafma/content','user','yntztn5krglcq4hcnjtq','user_upload','','',NULL,NULL,'2026-09-26 17:10:18.278568');
/*!40000 ALTER TABLE `artifact` ENABLE KEYS */;
DROP TABLE IF EXISTS `artifact_share`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `artifact_share` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `artifact_id` bigint unsigned NOT NULL,
  `space_id` bigint unsigned NOT NULL,
  `token_sha256` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_by_type` varchar(32) NOT NULL,
  `created_by_id` varchar(64) DEFAULT NULL,
  `expires_at` datetime(6) DEFAULT NULL,
  `revoked_at` datetime(6) DEFAULT NULL,
  `retrieval_count` bigint NOT NULL,
  `last_retrieved_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_artifact_share_public_id` (`public_id`),
  UNIQUE KEY `uq_artifact_share_token` (`token_sha256`),
  KEY `idx_artifact_share_artifact` (`artifact_id`),
  KEY `idx_artifact_share_space_created` (`space_id`,`created_at`),
  KEY `idx_artifact_share_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `artifact_share` DISABLE KEYS */;
/*!40000 ALTER TABLE `artifact_share` ENABLE KEYS */;
DROP TABLE IF EXISTS `audit_event`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `audit_event` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `space_id` bigint unsigned DEFAULT NULL,
  `created_at` datetime(6) NOT NULL,
  `actor_type` varchar(16) NOT NULL,
  `actor_id` varchar(64) NOT NULL,
  `action` varchar(64) NOT NULL,
  `target_type` varchar(32) DEFAULT NULL,
  `target_id` varchar(64) DEFAULT NULL,
  `task_run_id` varchar(20) DEFAULT NULL,
  `detail` varchar(255) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_audit_event_public_id` (`public_id`),
  KEY `idx_audit_space_time` (`space_id`,`created_at`),
  KEY `idx_audit_event_actor_id` (`actor_id`),
  KEY `idx_audit_event_action` (`action`),
  KEY `idx_audit_event_task_run_id` (`task_run_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `audit_event` DISABLE KEYS */;
INSERT INTO `audit_event` VALUES (1,'46ovy6ya4auuzev6avuq',NULL,'2026-09-26 17:10:18.001053','system','buildmax-server','user.created','user','yntztn5krglcq4hcnjtq','','');
INSERT INTO `audit_event` VALUES (2,'m3kqriuf3jsbenydfdkq',NULL,'2026-09-26 17:10:18.219056','system','buildmax-server','user.login_code_issued','user','yntztn5krglcq4hcnjtq','','');
INSERT INTO `audit_event` VALUES (3,'omxl3bmdxqhytm2spbjq',NULL,'2026-09-26 17:10:18.231294','user','yntztn5krglcq4hcnjtq','user.login','platform','upgrade-fixture','','login_code');
INSERT INTO `audit_event` VALUES (4,'32ulbvl53uszuedodvka',2,'2026-09-26 17:10:18.234944','user','yntztn5krglcq4hcnjtq','space.created','space','eamqmi4tcsyeuy453pla','','free_trial');
INSERT INTO `audit_event` VALUES (5,'jkqv6ukjer2utbo3zq7a',2,'2026-09-26 17:10:18.239436','user','yntztn5krglcq4hcnjtq','agent.created','agent','na4tx2mzqugn5pphagsq','','Upgrade fixture agent');
INSERT INTO `audit_event` VALUES (6,'nevl53np4pxepl5punda',2,'2026-09-26 17:10:18.249845','user','yntztn5krglcq4hcnjtq','workflow.created','workflow','5is6rbrm36itdiwc2fda','','Upgrade fixture workflow');
INSERT INTO `audit_event` VALUES (7,'zplfzdwuhl4snhkgc3ca',2,'2026-09-26 17:10:18.254041','user','yntztn5krglcq4hcnjtq','workflow.published','workflow','5is6rbrm36itdiwc2fda','','Upgrade fixture workflow');
INSERT INTO `audit_event` VALUES (8,'lp47bodsfxpjizq6pcmq',2,'2026-09-26 17:10:18.279887','user','yntztn5krglcq4hcnjtq','artifact.created','artifact','tfrqafpxhgkxhgfaafma','','user_upload');
/*!40000 ALTER TABLE `audit_event` ENABLE KEYS */;
DROP TABLE IF EXISTS `auth_session`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `auth_session` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `platform` varchar(32) DEFAULT NULL,
  `auth_method` varchar(32) DEFAULT NULL,
  `absolute_expires_at` datetime(6) NOT NULL,
  `last_seen_at` datetime(6) DEFAULT NULL,
  `revoked_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_auth_session_public_id` (`public_id`),
  KEY `idx_auth_session_user_id` (`user_id`),
  KEY `idx_auth_session_absolute_expires_at` (`absolute_expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `auth_session` DISABLE KEYS */;
INSERT INTO `auth_session` VALUES (1,'cxv4zsf7v32il2nc3kdq',1,'upgrade-fixture','login_code','2026-12-25 17:10:18.228086',NULL,NULL,'2026-09-26 17:10:18.228290');
/*!40000 ALTER TABLE `auth_session` ENABLE KEYS */;
DROP TABLE IF EXISTS `channel_identity`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `channel_identity` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `platform` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `tenant` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `external_user_id` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `handle` varchar(255) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_channel_identity_public_id` (`public_id`),
  UNIQUE KEY `uq_channel_identity_external` (`platform`,`tenant`,`external_user_id`),
  KEY `idx_channel_identity_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `channel_identity` DISABLE KEYS */;
/*!40000 ALTER TABLE `channel_identity` ENABLE KEYS */;
DROP TABLE IF EXISTS `channel_pairing`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `channel_pairing` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `code_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `platform` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `tenant` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `external_user_id` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `chat_id` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `handle` varchar(255) DEFAULT NULL,
  `expires_at` datetime(6) NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_channel_pairing_code_hash` (`code_hash`),
  UNIQUE KEY `uq_channel_pairing_external` (`platform`,`tenant`,`external_user_id`),
  KEY `idx_channel_pairing_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `channel_pairing` DISABLE KEYS */;
/*!40000 ALTER TABLE `channel_pairing` ENABLE KEYS */;
DROP TABLE IF EXISTS `conversation`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `conversation` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `space_id` bigint unsigned DEFAULT NULL,
  `channel` varchar(32) NOT NULL,
  `channel_ref` varchar(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  `title` varchar(256) DEFAULT NULL,
  `created_by` bigint unsigned NOT NULL,
  `turn_fence` bigint NOT NULL DEFAULT '0',
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_conversation_public_id` (`public_id`),
  KEY `idx_conversation_user_created` (`user_id`,`created_at`),
  KEY `idx_conversation_space_created` (`space_id`,`created_at`),
  KEY `idx_conversation_channel_ref` (`channel_ref`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `conversation` DISABLE KEYS */;
/*!40000 ALTER TABLE `conversation` ENABLE KEYS */;
DROP TABLE IF EXISTS `conversation_message`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `conversation_message` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `conversation_id` bigint unsigned NOT NULL,
  `role` varchar(16) NOT NULL,
  `content` text NOT NULL,
  `channel` varchar(32) DEFAULT NULL,
  `tool_call_id` varchar(64) DEFAULT NULL,
  `tool_calls` text,
  `provider_state` text,
  `parts` mediumtext,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_conversation_message_public_id` (`public_id`),
  KEY `idx_conversation_message_conversation` (`conversation_id`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `conversation_message` DISABLE KEYS */;
/*!40000 ALTER TABLE `conversation_message` ENABLE KEYS */;
DROP TABLE IF EXISTS `external_identity`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `external_identity` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `issuer` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `subject` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `last_seen_email` varchar(320) DEFAULT NULL,
  `last_seen_name` varchar(255) DEFAULT NULL,
  `last_login_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_external_identity_public_id` (`public_id`),
  UNIQUE KEY `uq_external_identity_issuer_user` (`issuer`,`user_id`),
  UNIQUE KEY `uq_external_identity_issuer_subject` (`issuer`,`subject`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `external_identity` DISABLE KEYS */;
/*!40000 ALTER TABLE `external_identity` ENABLE KEYS */;
DROP TABLE IF EXISTS `issue`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `issue` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `space_id` bigint unsigned DEFAULT NULL,
  `parent_issue_id` bigint unsigned DEFAULT NULL,
  `title` varchar(255) NOT NULL,
  `description` text NOT NULL,
  `status` varchar(32) NOT NULL,
  `owner_id` bigint unsigned DEFAULT NULL,
  `executor_kind` varchar(32) DEFAULT NULL,
  `executor_id` varchar(64) DEFAULT NULL,
  `created_by` bigint unsigned NOT NULL,
  `version` bigint unsigned NOT NULL DEFAULT '1',
  `created_at` datetime(6) DEFAULT NULL,
  `updated_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_issue_public_id` (`public_id`),
  KEY `idx_issue_user_id` (`user_id`),
  KEY `idx_issue_space_updated` (`space_id`,`updated_at`),
  KEY `idx_issue_parent_issue_id` (`parent_issue_id`),
  KEY `idx_issue_owner_id` (`owner_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `issue` DISABLE KEYS */;
INSERT INTO `issue` VALUES (1,'l5tc4ylqtszmt44kmcoq',1,2,NULL,'Upgrade fixture issue','Owned by a person, executed by an agent.','in_progress',1,'agent','na4tx2mzqugn5pphagsq',1,1,'2026-09-26 17:10:18.241762','2026-09-26 17:10:18.241762');
/*!40000 ALTER TABLE `issue` ENABLE KEYS */;
DROP TABLE IF EXISTS `issue_comment`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `issue_comment` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `issue_id` bigint unsigned NOT NULL,
  `author_kind` varchar(16) NOT NULL,
  `author_id` varchar(64) NOT NULL,
  `body` text NOT NULL,
  `source_task_id` bigint unsigned DEFAULT NULL,
  `source_task_run_id` bigint unsigned DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  `edited_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_issue_comment_public_id` (`public_id`),
  KEY `idx_issue_comment_issue_created` (`issue_id`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `issue_comment` DISABLE KEYS */;
INSERT INTO `issue_comment` VALUES (1,'te35uqhjerqowzim6kiq',1,'user','yntztn5krglcq4hcnjtq','A comment the upgrade must keep.',NULL,NULL,'2026-09-26 17:10:18.245642',NULL);
/*!40000 ALTER TABLE `issue_comment` ENABLE KEYS */;
DROP TABLE IF EXISTS `llm_call`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `llm_call` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `client_call_id` varchar(128) DEFAULT NULL,
  `user_id` bigint unsigned DEFAULT NULL,
  `task_run_id` bigint unsigned DEFAULT NULL,
  `surface` varchar(32) DEFAULT NULL,
  `session_id` varchar(64) DEFAULT NULL,
  `task_id` bigint unsigned DEFAULT NULL,
  `model` varchar(128) DEFAULT NULL,
  `target_id` varchar(64) NOT NULL,
  `provider_type` varchar(32) NOT NULL,
  `upstream_model` varchar(128) NOT NULL,
  `streaming` tinyint(1) NOT NULL DEFAULT '0',
  `accepted_at` datetime(6) NOT NULL,
  `upstream_started_at` datetime(6) DEFAULT NULL,
  `first_delta_at` datetime(6) DEFAULT NULL,
  `completed_at` datetime(6) DEFAULT NULL,
  `status` varchar(16) NOT NULL,
  `error_class` varchar(64) DEFAULT NULL,
  `attempts` bigint NOT NULL DEFAULT '0',
  `prompt_tokens` bigint DEFAULT NULL,
  `completion_tokens` bigint DEFAULT NULL,
  `total_tokens` bigint DEFAULT NULL,
  `cache_read_tokens` bigint DEFAULT NULL,
  `cache_write_tokens` bigint DEFAULT NULL,
  `usage_source` varchar(16) DEFAULT NULL,
  `currency` varchar(8) DEFAULT NULL,
  `rate_input_per_mtok` bigint DEFAULT NULL,
  `rate_cache_read_per_mtok` bigint DEFAULT NULL,
  `rate_cache_write_per_mtok` bigint DEFAULT NULL,
  `rate_output_per_mtok` bigint DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_llm_call_public_id` (`public_id`),
  UNIQUE KEY `idx_llm_call_client` (`user_id`,`client_call_id`),
  KEY `idx_llm_call_task_run_id` (`task_run_id`),
  KEY `idx_llm_call_task_id` (`task_id`),
  KEY `idx_llm_call_accepted_at` (`accepted_at`),
  KEY `idx_llm_call_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `llm_call` DISABLE KEYS */;
/*!40000 ALTER TABLE `llm_call` ENABLE KEYS */;
DROP TABLE IF EXISTS `llm_model`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `llm_model` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `name` varchar(128) NOT NULL,
  `provider_type` varchar(32) NOT NULL,
  `api_url` varchar(512) NOT NULL,
  `api_key_sealed` blob,
  `model` varchar(128) NOT NULL,
  `context_window` bigint NOT NULL DEFAULT '0',
  `call_timeout` bigint NOT NULL DEFAULT '0',
  `max_tokens` bigint NOT NULL DEFAULT '0',
  `reasoning` varchar(16) NOT NULL DEFAULT '',
  `cache_mode` varchar(16) NOT NULL DEFAULT '',
  `cache_ttl` varchar(16) NOT NULL DEFAULT '',
  `currency` varchar(8) NOT NULL DEFAULT '',
  `input_per_mtok` bigint NOT NULL DEFAULT '0',
  `cache_read_per_mtok` bigint NOT NULL DEFAULT '0',
  `cache_write_per_mtok` bigint NOT NULL DEFAULT '0',
  `output_per_mtok` bigint NOT NULL DEFAULT '0',
  `vision` tinyint(1) NOT NULL DEFAULT '0',
  `capabilities` varchar(255) DEFAULT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT '1',
  `created_at` datetime(6) DEFAULT NULL,
  `updated_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_llm_model_public_id` (`public_id`),
  UNIQUE KEY `idx_llm_model_name` (`name`),
  KEY `idx_llm_model_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `llm_model` DISABLE KEYS */;
/*!40000 ALTER TABLE `llm_model` ENABLE KEYS */;
DROP TABLE IF EXISTS `login_code`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `login_code` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `code_hash` varchar(128) NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `expires_at` datetime(6) NOT NULL,
  `used_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_login_code_code_hash` (`code_hash`),
  KEY `idx_login_code_user_id` (`user_id`),
  KEY `idx_login_code_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `login_code` DISABLE KEYS */;
INSERT INTO `login_code` VALUES (1,'4451816355e380aaa685f363f0bdae5149959aded1032b4debc3d7d809b7f229',1,'2026-09-26 18:10:18.218061','2026-09-26 17:10:18.225822','2026-09-26 17:10:18.218106');
/*!40000 ALTER TABLE `login_code` ENABLE KEYS */;
DROP TABLE IF EXISTS `plugin`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `plugin` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(128) NOT NULL,
  `display_name` varchar(255) NOT NULL DEFAULT '',
  `description` varchar(1024) NOT NULL DEFAULT '',
  `archived_at` datetime(6) DEFAULT NULL,
  `created_by` bigint unsigned NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  `updated_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_plugin_name` (`name`),
  KEY `idx_plugin_archived_at` (`archived_at`),
  KEY `idx_plugin_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `plugin` DISABLE KEYS */;
/*!40000 ALTER TABLE `plugin` ENABLE KEYS */;
DROP TABLE IF EXISTS `plugin_activation`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `plugin_activation` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `space_id` bigint unsigned NOT NULL,
  `plugin_name` varchar(128) NOT NULL,
  `version` varchar(64) NOT NULL,
  `digest` varchar(128) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT '1',
  `origin` varchar(16) NOT NULL DEFAULT 'curated',
  `activated_by` bigint unsigned NOT NULL,
  `activated_at` datetime(6) DEFAULT NULL,
  `updated_by` bigint unsigned NOT NULL,
  `updated_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_plugin_activation_public_id` (`public_id`),
  UNIQUE KEY `ux_plugin_activation_space_plugin` (`space_id`,`plugin_name`),
  KEY `idx_plugin_activation_activated_at` (`activated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `plugin_activation` DISABLE KEYS */;
/*!40000 ALTER TABLE `plugin_activation` ENABLE KEYS */;
DROP TABLE IF EXISTS `plugin_environment`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `plugin_environment` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `space_id` bigint unsigned NOT NULL,
  `task_id` bigint unsigned NOT NULL,
  `source_task_run_id` bigint unsigned NOT NULL,
  `base_environment_id` bigint unsigned DEFAULT NULL,
  `entries` text NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_plugin_environment_public_id` (`public_id`),
  UNIQUE KEY `uq_plugin_environment_run` (`source_task_run_id`),
  KEY `idx_plugin_environment_space_id` (`space_id`),
  KEY `idx_plugin_environment_task_created` (`task_id`,`created_at`),
  KEY `idx_plugin_environment_base_environment_id` (`base_environment_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `plugin_environment` DISABLE KEYS */;
/*!40000 ALTER TABLE `plugin_environment` ENABLE KEYS */;
DROP TABLE IF EXISTS `plugin_release`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `plugin_release` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `plugin_id` bigint unsigned NOT NULL,
  `plugin_name` varchar(128) NOT NULL,
  `version` varchar(64) NOT NULL,
  `min_buildmax_version` varchar(64) NOT NULL DEFAULT '',
  `digest` varchar(128) NOT NULL,
  `object_key` varchar(512) NOT NULL,
  `size_bytes` bigint NOT NULL DEFAULT '0',
  `inspection` text,
  `source` text,
  `published_by` bigint unsigned NOT NULL,
  `published_at` datetime(6) DEFAULT NULL,
  `yanked_at` datetime(6) DEFAULT NULL,
  `yanked_by` bigint unsigned DEFAULT NULL,
  `yanked_reason` varchar(512) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  UNIQUE KEY `ux_plugin_release_version` (`plugin_name`,`version`),
  KEY `idx_plugin_release_plugin_id` (`plugin_id`),
  KEY `idx_plugin_release_digest` (`digest`),
  KEY `idx_plugin_release_published_at` (`published_at`),
  KEY `idx_plugin_release_yanked_at` (`yanked_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `plugin_release` DISABLE KEYS */;
/*!40000 ALTER TABLE `plugin_release` ENABLE KEYS */;
DROP TABLE IF EXISTS `quota_tier`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `quota_tier` (
  `tier_name` varchar(64) NOT NULL,
  `max_runs_per_period` bigint NOT NULL,
  `max_tokens_per_period` bigint NOT NULL,
  `max_storage_bytes` bigint NOT NULL DEFAULT '0',
  `period_days` bigint NOT NULL,
  PRIMARY KEY (`tier_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `quota_tier` DISABLE KEYS */;
INSERT INTO `quota_tier` VALUES ('free_trial',10,100000,0,30);
INSERT INTO `quota_tier` VALUES ('pro',1000,10000000,0,30);
/*!40000 ALTER TABLE `quota_tier` ENABLE KEYS */;
DROP TABLE IF EXISTS `remote_session`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `remote_session` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `display_name` varchar(200) DEFAULT NULL,
  `platform` varchar(32) DEFAULT NULL,
  `host` varchar(200) DEFAULT NULL,
  `status` varchar(16) NOT NULL,
  `last_seen_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_remote_session_public_id` (`public_id`),
  KEY `idx_remote_session_user_id` (`user_id`),
  KEY `idx_remote_session_status` (`status`),
  KEY `idx_remote_session_last_seen_at` (`last_seen_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `remote_session` DISABLE KEYS */;
/*!40000 ALTER TABLE `remote_session` ENABLE KEYS */;
DROP TABLE IF EXISTS `schedule`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `schedule` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `space_id` bigint unsigned NOT NULL,
  `executor_kind` varchar(32) NOT NULL DEFAULT '',
  `executor_id` varchar(64) NOT NULL DEFAULT '',
  `created_by` bigint unsigned NOT NULL,
  `name` varchar(256) DEFAULT NULL,
  `input` text NOT NULL,
  `cron_expr` varchar(256) NOT NULL,
  `timezone` varchar(64) NOT NULL,
  `enabled` tinyint(1) NOT NULL,
  `pause_reason` varchar(32) NOT NULL DEFAULT '',
  `next_fire_at` datetime(6) NOT NULL,
  `last_fire_at` datetime(6) DEFAULT NULL,
  `last_fire_ref` varchar(64) DEFAULT NULL,
  `consecutive_failures` bigint NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  `updated_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_schedule_public_id` (`public_id`),
  KEY `idx_schedule_space_created` (`space_id`,`created_at`),
  KEY `idx_schedule_due` (`enabled`,`next_fire_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `schedule` DISABLE KEYS */;
INSERT INTO `schedule` VALUES (1,'r452dfe74cb5tong4lja',2,'agent','na4tx2mzqugn5pphagsq',1,'Every minute','Summarize new issues.','* * * * *','UTC',0,'manual','2026-09-26 17:12:00.000000','2026-09-26 17:11:16.274977','lb37ups4s63epke65vnq',0,'2026-09-26 17:10:18.271165','2026-09-26 17:11:16.467115');
INSERT INTO `schedule` VALUES (2,'berul43d2227hhg5q4zq',2,'agent','na4tx2mzqugn5pphagsq',1,'Yearly','Summarize new issues.','0 0 1 1 *','UTC',1,'','2027-01-01 00:00:00.000000',NULL,NULL,0,'2026-09-26 17:10:18.273703','2026-09-26 17:10:18.273703');
/*!40000 ALTER TABLE `schedule` ENABLE KEYS */;
DROP TABLE IF EXISTS `schema_migration`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `schema_migration` (
  `id` varchar(191) NOT NULL,
  `applied_at` datetime(6) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `schema_migration` DISABLE KEYS */;
INSERT INTO `schema_migration` VALUES ('issue_owner_executor_split','2026-09-26 17:10:16.270404');
INSERT INTO `schema_migration` VALUES ('llm_model_credential_encryption','2026-09-26 17:10:16.267637');
INSERT INTO `schema_migration` VALUES ('schedule_agent_to_executor','2026-09-26 17:10:16.273032');
INSERT INTO `schema_migration` VALUES ('system_grant_live_marker','2026-09-26 17:10:16.265838');
INSERT INTO `schema_migration` VALUES ('workflow_step_run_to_node_run','2026-09-26 17:10:16.271794');
/*!40000 ALTER TABLE `schema_migration` ENABLE KEYS */;
DROP TABLE IF EXISTS `secret`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `secret` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `space_id` bigint unsigned NOT NULL,
  `name` varchar(128) NOT NULL,
  `description` varchar(1024) NOT NULL DEFAULT '',
  `provider` varchar(32) NOT NULL DEFAULT 'embedded',
  `state` varchar(16) NOT NULL DEFAULT 'active',
  `item_names` text NOT NULL,
  `ciphertext` blob,
  `nonce` varbinary(64) DEFAULT NULL,
  `wrapped_dek` varbinary(256) DEFAULT NULL,
  `key_id` varchar(128) NOT NULL DEFAULT '',
  `created_by` bigint unsigned NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  `updated_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_secret_public_id` (`public_id`),
  UNIQUE KEY `ux_secret_space_name` (`space_id`,`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `secret` DISABLE KEYS */;
/*!40000 ALTER TABLE `secret` ENABLE KEYS */;
DROP TABLE IF EXISTS `space`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `space` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `name` varchar(255) NOT NULL,
  `personal_for_user_id` bigint unsigned DEFAULT NULL,
  `quota_tier` varchar(64) DEFAULT NULL,
  `plugin_curation` varchar(16) NOT NULL DEFAULT 'open',
  `agent_instructions` text,
  `agent_instructions_revision` bigint NOT NULL DEFAULT '0',
  `default_sandbox_network_tier` varchar(64) DEFAULT NULL,
  `default_sandbox_filesystem_tier` varchar(64) DEFAULT NULL,
  `created_by` bigint unsigned NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  `updated_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_space_public_id` (`public_id`),
  UNIQUE KEY `idx_space_personal_for_user_id` (`personal_for_user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `space` DISABLE KEYS */;
INSERT INTO `space` VALUES (1,'z5gx5k3ncuk6awl6phtq','My Space',1,'free_trial','open','',0,'','',1,'2026-09-26 17:10:17.999624','2026-09-26 17:10:17.999624');
INSERT INTO `space` VALUES (2,'eamqmi4tcsyeuy453pla','Upgrade fixture',NULL,'free_trial','open','',0,'','',1,'2026-09-26 17:10:18.233638','2026-09-26 17:10:18.233638');
/*!40000 ALTER TABLE `space` ENABLE KEYS */;
DROP TABLE IF EXISTS `space_invitation`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `space_invitation` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `space_id` bigint unsigned NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `role` varchar(32) NOT NULL,
  `invited_by` bigint unsigned NOT NULL,
  `expires_at` datetime(6) NOT NULL,
  `accepted_at` datetime(6) DEFAULT NULL,
  `revoked_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_space_invitation_public_id` (`public_id`),
  KEY `idx_space_invitation_space_id` (`space_id`),
  KEY `idx_space_invitation_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `space_invitation` DISABLE KEYS */;
/*!40000 ALTER TABLE `space_invitation` ENABLE KEYS */;
DROP TABLE IF EXISTS `space_member`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `space_member` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `space_id` bigint unsigned NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `role` varchar(32) NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_space_member_space_user` (`space_id`,`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `space_member` DISABLE KEYS */;
INSERT INTO `space_member` VALUES (1,1,1,'owner','2026-09-26 17:10:17.999624');
INSERT INTO `space_member` VALUES (2,2,1,'owner','2026-09-26 17:10:18.233638');
/*!40000 ALTER TABLE `space_member` ENABLE KEYS */;
DROP TABLE IF EXISTS `system_grant`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `system_grant` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `role` varchar(32) NOT NULL,
  `revoked_at` datetime(6) DEFAULT NULL,
  `live_marker` tinyint unsigned DEFAULT NULL,
  `granted_by` varchar(64) NOT NULL,
  `granted_at` datetime(6) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_system_grant_public_id` (`public_id`),
  UNIQUE KEY `idx_system_grant_live` (`user_id`,`role`,`live_marker`),
  KEY `idx_system_grant_user` (`user_id`),
  KEY `idx_system_grant_granted_at` (`granted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `system_grant` DISABLE KEYS */;
/*!40000 ALTER TABLE `system_grant` ENABLE KEYS */;
DROP TABLE IF EXISTS `task`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `task` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `conversation_id` bigint unsigned DEFAULT NULL,
  `space_id` bigint unsigned NOT NULL,
  `issue_id` bigint unsigned DEFAULT NULL,
  `schedule_id` bigint unsigned DEFAULT NULL,
  `status` varchar(32) NOT NULL,
  `input` text NOT NULL,
  `title` varchar(256) DEFAULT NULL,
  `title_prompt_tokens` bigint DEFAULT NULL,
  `title_completion_tokens` bigint DEFAULT NULL,
  `output` text,
  `output_schema` text,
  `created_by` bigint unsigned NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  `started_at` datetime(6) DEFAULT NULL,
  `ended_at` datetime(6) DEFAULT NULL,
  `error_message` text,
  `session_id` varchar(36) DEFAULT NULL,
  `last_run_id` bigint unsigned DEFAULT NULL,
  `agent_id` bigint unsigned DEFAULT NULL,
  `workspace_head_checkpoint_id` bigint unsigned DEFAULT NULL,
  `plugin_environment_head_id` bigint unsigned DEFAULT NULL,
  `admission_key` varchar(191) DEFAULT NULL,
  `admission_fingerprint` char(64) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_task_public_id` (`public_id`),
  UNIQUE KEY `uq_task_admission_key` (`space_id`,`admission_key`),
  KEY `idx_task_conversation_id` (`conversation_id`),
  KEY `idx_task_space_created` (`space_id`,`created_at`),
  KEY `idx_task_issue_id` (`issue_id`),
  KEY `idx_task_schedule_id` (`schedule_id`),
  KEY `idx_task_last_run_id` (`last_run_id`),
  KEY `idx_task_agent_id` (`agent_id`),
  KEY `idx_task_workspace_head_checkpoint_id` (`workspace_head_checkpoint_id`),
  KEY `idx_task_plugin_environment_head_id` (`plugin_environment_head_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `task` DISABLE KEYS */;
INSERT INTO `task` VALUES (1,'3zj3pgtkw7tom65tfjra',NULL,2,NULL,NULL,'FAILED','Agent: Upgrade fixture agent\nDescription: Seeded by the upgrade fixture.\nInstructions:\nSummarize the issue.\n\nSummarize the issue.','Agent: Upgrade fixture agent\nDescription: Seeded b…',0,0,NULL,NULL,1,'2026-09-26 17:10:18.264004',NULL,'2026-09-26 17:10:21.289657','exit status 1','d7bd5ca8-34de-4b21-8668-0c8ae5a4ea47',1,1,NULL,NULL,'workflow/orxn4oxxqk5ut5i3cz6a/node/summarize','ba9606b870008c331cc34168872ab7cda1e0e71dd57d026662b824d670d858a2');
INSERT INTO `task` VALUES (2,'lb37ups4s63epke65vnq',NULL,2,NULL,1,'FAILED','Summarize new issues.','Summarize new issues.',0,0,NULL,NULL,1,'2026-09-26 17:11:16.297630',NULL,'2026-09-26 17:11:21.285672','exit status 1','4dfe611f-97d9-415e-af7f-3ca05a0cd027',2,1,NULL,NULL,NULL,NULL);
/*!40000 ALTER TABLE `task` ENABLE KEYS */;
DROP TABLE IF EXISTS `task_run`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `task_run` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `task_id` bigint unsigned NOT NULL,
  `previous_task_run_id` bigint unsigned DEFAULT NULL,
  `input` text NOT NULL,
  `idempotency_key` varchar(128) DEFAULT NULL,
  `created_by` varchar(64) DEFAULT NULL,
  `created_by_type` varchar(32) DEFAULT NULL,
  `trigger_source` varchar(64) DEFAULT NULL,
  `status` varchar(32) NOT NULL,
  `output` text,
  `structured` text,
  `error_message` text,
  `started_at` datetime(6) DEFAULT NULL,
  `ended_at` datetime(6) DEFAULT NULL,
  `session_id` varchar(36) DEFAULT NULL,
  `worker_type` varchar(32) DEFAULT NULL,
  `k8s_job_name` varchar(128) DEFAULT NULL,
  `k8s_job_created_at` datetime(6) DEFAULT NULL,
  `prompt_tokens` bigint DEFAULT NULL,
  `completion_tokens` bigint DEFAULT NULL,
  `trace_path` varchar(512) DEFAULT NULL,
  `cancel_requested_at` datetime(6) DEFAULT NULL,
  `cancel_requested_by` bigint unsigned DEFAULT NULL,
  `cancel_reason` varchar(32) NOT NULL DEFAULT '',
  `retry_of_task_run_id` bigint unsigned DEFAULT NULL,
  `source_message_id` bigint unsigned DEFAULT NULL,
  `agent_revision` bigint DEFAULT NULL,
  `space_agent_instructions_revision` bigint DEFAULT NULL,
  `plugin_pins` text,
  `sandbox_network_tier` varchar(64) DEFAULT NULL,
  `sandbox_filesystem_tier` varchar(64) DEFAULT NULL,
  `last_seen_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  `workspace_base_checkpoint_id` bigint unsigned DEFAULT NULL,
  `workspace_result_checkpoint_id` bigint unsigned DEFAULT NULL,
  `workspace_partial_checkpoint_id` bigint unsigned DEFAULT NULL,
  `workspace_restore_status` varchar(32) DEFAULT NULL,
  `workspace_restore_error` text,
  `workspace_checkpoint_status` varchar(32) DEFAULT NULL,
  `workspace_checkpoint_error` text,
  `plugin_environment_base_id` bigint unsigned DEFAULT NULL,
  `plugin_environment_result_id` bigint unsigned DEFAULT NULL,
  `plugin_environment_status` varchar(32) DEFAULT NULL,
  `plugin_environment_error` text,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_task_run_public_id` (`public_id`),
  UNIQUE KEY `idx_task_run_idempotency` (`task_id`,`idempotency_key`),
  KEY `idx_task_run_task_created` (`task_id`,`created_at`),
  KEY `idx_task_run_previous_task_run_id` (`previous_task_run_id`),
  KEY `idx_task_run_created_by` (`created_by`),
  KEY `idx_task_run_cancel_requested_at` (`cancel_requested_at`),
  KEY `idx_task_run_retry_of_task_run_id` (`retry_of_task_run_id`),
  KEY `idx_task_run_source_message_id` (`source_message_id`),
  KEY `idx_task_run_last_seen_at` (`last_seen_at`),
  KEY `idx_task_run_workspace_base_checkpoint_id` (`workspace_base_checkpoint_id`),
  KEY `idx_task_run_workspace_result_checkpoint_id` (`workspace_result_checkpoint_id`),
  KEY `idx_task_run_workspace_partial_checkpoint_id` (`workspace_partial_checkpoint_id`),
  KEY `idx_task_run_plugin_environment_base_id` (`plugin_environment_base_id`),
  KEY `idx_task_run_plugin_environment_result_id` (`plugin_environment_result_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `task_run` DISABLE KEYS */;
INSERT INTO `task_run` VALUES (1,'ih3vswo7qujm5psiucaa',1,NULL,'Agent: Upgrade fixture agent\nDescription: Seeded by the upgrade fixture.\nInstructions:\nSummarize the issue.\n\nSummarize the issue.',NULL,'yntztn5krglcq4hcnjtq','user','workflow_step','FAILED',NULL,NULL,'exit status 1',NULL,'2026-09-26 17:10:21.289657',NULL,'',NULL,NULL,NULL,NULL,NULL,NULL,NULL,'',NULL,NULL,1,NULL,'','','',NULL,'2026-09-26 17:10:18.264004',NULL,NULL,NULL,'',NULL,'',NULL,NULL,NULL,'',NULL);
INSERT INTO `task_run` VALUES (2,'c2alxkxyuxyj2t3bpvea',2,NULL,'Summarize new issues.',NULL,'yntztn5krglcq4hcnjtq','system','schedule','FAILED',NULL,NULL,'exit status 1',NULL,'2026-09-26 17:11:21.285672',NULL,'',NULL,NULL,NULL,NULL,NULL,NULL,NULL,'',NULL,NULL,1,NULL,'','','',NULL,'2026-09-26 17:11:16.297630',NULL,NULL,NULL,'',NULL,'',NULL,NULL,NULL,'',NULL);
/*!40000 ALTER TABLE `task_run` ENABLE KEYS */;
DROP TABLE IF EXISTS `task_run_secret`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `task_run_secret` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_run_id` bigint unsigned NOT NULL,
  `secret_id` bigint unsigned NOT NULL,
  `item_name` varchar(128) NOT NULL,
  `agent_id` bigint unsigned NOT NULL,
  `agent_revision` bigint NOT NULL,
  `delivery` varchar(8) NOT NULL,
  `env_name` varchar(256) NOT NULL DEFAULT '',
  `file_target` varchar(512) NOT NULL DEFAULT '',
  `status` varchar(16) NOT NULL DEFAULT 'pending',
  `expires_at` datetime(6) DEFAULT NULL,
  `materialized_at` datetime(6) DEFAULT NULL,
  `revoked_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `ux_task_run_secret` (`task_run_id`,`secret_id`,`item_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `task_run_secret` DISABLE KEYS */;
/*!40000 ALTER TABLE `task_run_secret` ENABLE KEYS */;
DROP TABLE IF EXISTS `user`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `user` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `email` varchar(255) NOT NULL,
  `name` varchar(255) DEFAULT NULL,
  `password_hash` varchar(255) DEFAULT NULL,
  `password_set_at` datetime(6) DEFAULT NULL,
  `quota_tier` varchar(64) DEFAULT NULL,
  `last_login_at` datetime(6) DEFAULT NULL,
  `last_login_platform` varchar(32) DEFAULT NULL,
  `disabled_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_user_public_id` (`public_id`),
  UNIQUE KEY `idx_user_email` (`email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `user` DISABLE KEYS */;
INSERT INTO `user` VALUES (1,'yntztn5krglcq4hcnjtq','upgrade-owner@buildmax.local','',NULL,NULL,'free_trial','2026-09-26 17:10:18.228086','upgrade-fixture',NULL,'2026-09-26 17:10:17.999624');
/*!40000 ALTER TABLE `user` ENABLE KEYS */;
DROP TABLE IF EXISTS `user_refresh_token`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `user_refresh_token` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `token_hash` varchar(128) NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `session_id` varchar(64) NOT NULL,
  `platform` varchar(32) DEFAULT NULL,
  `expires_at` datetime(6) NOT NULL,
  `used_at` datetime(6) DEFAULT NULL,
  `revoked_at` datetime(6) DEFAULT NULL,
  `replaced_by` varchar(128) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_user_refresh_token_token_hash` (`token_hash`),
  KEY `idx_user_refresh_token_user_id` (`user_id`),
  KEY `idx_user_refresh_token_session_id` (`session_id`),
  KEY `idx_user_refresh_token_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `user_refresh_token` DISABLE KEYS */;
INSERT INTO `user_refresh_token` VALUES (1,'e0942fb8122f55857e612fd08395bdd339eaa08c3c210992d52eb6c69ec13bf5',1,'cxv4zsf7v32il2nc3kdq','upgrade-fixture','2026-10-26 17:10:18.229498',NULL,NULL,'','2026-09-26 17:10:18.229535');
/*!40000 ALTER TABLE `user_refresh_token` ENABLE KEYS */;
DROP TABLE IF EXISTS `user_webhook_key`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `user_webhook_key` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `key_hash` varchar(128) NOT NULL,
  `name` varchar(255) DEFAULT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_user_webhook_key_public_id` (`public_id`),
  UNIQUE KEY `idx_user_webhook_key_key_hash` (`key_hash`),
  KEY `idx_user_webhook_key_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `user_webhook_key` DISABLE KEYS */;
/*!40000 ALTER TABLE `user_webhook_key` ENABLE KEYS */;
DROP TABLE IF EXISTS `workflow`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `workflow` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `space_id` bigint unsigned NOT NULL,
  `name` varchar(255) NOT NULL,
  `description` text NOT NULL,
  `definition` longtext NOT NULL,
  `status` varchar(32) NOT NULL DEFAULT 'draft',
  `revision` bigint NOT NULL DEFAULT '1',
  `created_by` bigint unsigned NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  `updated_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_workflow_public_id` (`public_id`),
  KEY `idx_workflow_space_id` (`space_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `workflow` DISABLE KEYS */;
INSERT INTO `workflow` VALUES (1,'5is6rbrm36itdiwc2fda',2,'Upgrade fixture workflow','One agent step.','{\"schema_version\":1,\"nodes\":[{\"id\":\"summarize\",\"type\":\"agent_task\",\"agent\":{\"id\":\"na4tx2mzqugn5pphagsq\",\"revision\":1},\"input\":{\"instruction\":\"Summarize the issue.\"},\"issue_access\":\"none\"}]}','published',2,1,'2026-09-26 17:10:18.248356','2026-09-26 17:10:18.252390');
/*!40000 ALTER TABLE `workflow` ENABLE KEYS */;
DROP TABLE IF EXISTS `workflow_node_run`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `workflow_node_run` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `workflow_run_id` bigint unsigned NOT NULL,
  `node_id` varchar(128) NOT NULL,
  `node_index` bigint NOT NULL,
  `node_type` varchar(32) NOT NULL,
  `target_agent_id` bigint unsigned DEFAULT NULL,
  `agent_name` varchar(255) NOT NULL,
  `agent_description` text NOT NULL,
  `agent_instructions` longtext NOT NULL,
  `agent_revision` bigint NOT NULL DEFAULT '0',
  `needs` text,
  `issue_access` varchar(16) NOT NULL DEFAULT '',
  `prompt` text NOT NULL,
  `bindings` text,
  `output_schema` text,
  `status` varchar(32) NOT NULL,
  `task_id` bigint unsigned DEFAULT NULL,
  `task_run_id` bigint unsigned DEFAULT NULL,
  `resolved_input` longtext,
  `output` longtext,
  `structured` text,
  `error_message` text,
  `created_at` datetime(6) DEFAULT NULL,
  `started_at` datetime(6) DEFAULT NULL,
  `ended_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_workflow_node_run_public_id` (`public_id`),
  KEY `idx_node_run_run_index` (`workflow_run_id`,`node_index`),
  KEY `idx_workflow_node_run_target_agent_id` (`target_agent_id`),
  KEY `idx_workflow_node_run_task_id` (`task_id`),
  KEY `idx_workflow_node_run_task_run_id` (`task_run_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `workflow_node_run` DISABLE KEYS */;
INSERT INTO `workflow_node_run` VALUES (1,'e573pmgpwht5swn4rraa',1,'summarize',0,'agent_task',1,'Upgrade fixture agent','Seeded by the upgrade fixture.','Summarize the issue.',1,NULL,'none','Summarize the issue.',NULL,NULL,'failed',1,1,'Agent: Upgrade fixture agent\nDescription: Seeded by the upgrade fixture.\nInstructions:\nSummarize the issue.\n\nSummarize the issue.',NULL,NULL,'exit status 1','2026-09-26 17:10:18.259948','2026-09-26 17:10:18.262959','2026-09-26 17:11:16.285879');
/*!40000 ALTER TABLE `workflow_node_run` ENABLE KEYS */;
DROP TABLE IF EXISTS `workflow_revision`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `workflow_revision` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `workflow_id` bigint unsigned NOT NULL,
  `revision` bigint NOT NULL,
  `name` varchar(255) NOT NULL,
  `description` text NOT NULL,
  `definition` longtext NOT NULL,
  `status` varchar(32) NOT NULL,
  `created_by` bigint unsigned NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_workflow_revision` (`workflow_id`,`revision`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `workflow_revision` DISABLE KEYS */;
INSERT INTO `workflow_revision` VALUES (1,1,1,'Upgrade fixture workflow','One agent step.','{\"nodes\":[{\"agent\":{\"id\":\"na4tx2mzqugn5pphagsq\"},\"id\":\"summarize\",\"input\":{\"instruction\":\"Summarize the issue.\"},\"type\":\"agent_task\"}],\"schema_version\":1}','draft',1,'2026-09-26 17:10:18.248755');
INSERT INTO `workflow_revision` VALUES (2,1,2,'Upgrade fixture workflow','One agent step.','{\"schema_version\":1,\"nodes\":[{\"id\":\"summarize\",\"type\":\"agent_task\",\"agent\":{\"id\":\"na4tx2mzqugn5pphagsq\",\"revision\":1},\"input\":{\"instruction\":\"Summarize the issue.\"},\"issue_access\":\"none\"}]}','published',1,'2026-09-26 17:10:18.252822');
/*!40000 ALTER TABLE `workflow_revision` ENABLE KEYS */;
DROP TABLE IF EXISTS `workflow_run`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `workflow_run` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `workflow_id` bigint unsigned NOT NULL,
  `workflow_revision` bigint NOT NULL DEFAULT '0',
  `issue_id` bigint unsigned DEFAULT NULL,
  `schedule_id` bigint unsigned DEFAULT NULL,
  `input` longtext,
  `status` varchar(32) NOT NULL,
  `result_json` longtext,
  `created_by` bigint unsigned NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  `started_at` datetime(6) DEFAULT NULL,
  `ended_at` datetime(6) DEFAULT NULL,
  `error_message` text,
  `reconcile_owner` varchar(64) DEFAULT NULL,
  `lease_expires_at` datetime(6) DEFAULT NULL,
  `next_reconcile_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_workflow_run_public_id` (`public_id`),
  KEY `idx_workflow_run_workflow_created` (`workflow_id`,`created_at`),
  KEY `idx_workflow_run_issue_id` (`issue_id`),
  KEY `idx_workflow_run_schedule_id` (`schedule_id`),
  KEY `idx_workflow_run_lease_expires` (`lease_expires_at`),
  KEY `idx_workflow_run_next_reconcile` (`next_reconcile_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `workflow_run` DISABLE KEYS */;
INSERT INTO `workflow_run` VALUES (1,'orxn4oxxqk5ut5i3cz6a',1,2,NULL,NULL,NULL,'failed',NULL,1,'2026-09-26 17:10:18.257247','2026-09-26 17:10:18.257247','2026-09-26 17:11:16.285879','exit status 1',NULL,NULL,NULL);
/*!40000 ALTER TABLE `workflow_run` ENABLE KEYS */;
DROP TABLE IF EXISTS `workspace_checkpoint`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `workspace_checkpoint` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `space_id` bigint unsigned NOT NULL,
  `task_id` bigint unsigned NOT NULL,
  `source_task_run_id` bigint unsigned NOT NULL,
  `kind` varchar(32) NOT NULL,
  `base_checkpoint_id` bigint unsigned DEFAULT NULL,
  `payload_format` varchar(32) NOT NULL,
  `payload_sha256` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `storage_key` varchar(1024) NOT NULL,
  `size_bytes` bigint NOT NULL,
  `uncompressed_bytes` bigint NOT NULL,
  `entry_count` bigint NOT NULL,
  `created_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_workspace_checkpoint_public_id` (`public_id`),
  UNIQUE KEY `uq_workspace_checkpoint_run_kind` (`source_task_run_id`,`kind`),
  KEY `idx_workspace_checkpoint_space_id` (`space_id`),
  KEY `idx_workspace_checkpoint_task_created` (`task_id`,`created_at`),
  KEY `idx_workspace_checkpoint_base_checkpoint_id` (`base_checkpoint_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

/*!40000 ALTER TABLE `workspace_checkpoint` DISABLE KEYS */;
/*!40000 ALTER TABLE `workspace_checkpoint` ENABLE KEYS */;
/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;

/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;
