--
-- PostgreSQL database dump
--


-- Dumped from database version 16.14 (Debian 16.14-1.pgdg13+1)
-- Dumped by pg_dump version 16.14 (Debian 16.14-1.pgdg13+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: curio; Type: SCHEMA; Schema: -; Owner: -
--



--
-- Name: adjust_data_set_refcount_on_update(); Type: FUNCTION; Schema: curio; Owner: -
--

CREATE FUNCTION adjust_data_set_refcount_on_update() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.pdp_pieceref IS DISTINCT FROM NEW.pdp_pieceref THEN
        IF OLD.pdp_pieceref IS NOT NULL THEN
            UPDATE pdp_piecerefs
            SET data_set_refcount = data_set_refcount - 1
            WHERE id = OLD.pdp_pieceref;
        END IF;
        IF NEW.pdp_pieceref IS NOT NULL THEN
            UPDATE pdp_piecerefs
            SET data_set_refcount = data_set_refcount + 1
            WHERE id = NEW.pdp_pieceref;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;


--
-- Name: adjust_parked_piece_ref_count_on_update(); Type: FUNCTION; Schema: curio; Owner: -
--

CREATE FUNCTION adjust_parked_piece_ref_count_on_update() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.piece_id IS DISTINCT FROM NEW.piece_id THEN
        UPDATE parked_pieces
        SET ref_count = ref_count - 1
        WHERE id = OLD.piece_id;
        UPDATE parked_pieces
        SET ref_count = ref_count + 1
        WHERE id = NEW.piece_id;
    END IF;
    RETURN NEW;
END;
$$;


--
-- Name: decrement_data_set_refcount(); Type: FUNCTION; Schema: curio; Owner: -
--

CREATE FUNCTION decrement_data_set_refcount() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    UPDATE pdp_piecerefs
    SET data_set_refcount = data_set_refcount - 1
    WHERE id = OLD.pdp_pieceref;
    RETURN OLD;
END;
$$;


--
-- Name: decrement_parked_piece_ref_count(); Type: FUNCTION; Schema: curio; Owner: -
--

CREATE FUNCTION decrement_parked_piece_ref_count() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    UPDATE parked_pieces
    SET ref_count = ref_count - 1
    WHERE id = OLD.piece_id;
    RETURN OLD;
END;
$$;


--
-- Name: increment_data_set_refcount(); Type: FUNCTION; Schema: curio; Owner: -
--

CREATE FUNCTION increment_data_set_refcount() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    UPDATE pdp_piecerefs
    SET data_set_refcount = data_set_refcount + 1
    WHERE id = NEW.pdp_pieceref;
    RETURN NEW;
END;
$$;


--
-- Name: increment_parked_piece_ref_count(); Type: FUNCTION; Schema: curio; Owner: -
--

CREATE FUNCTION increment_parked_piece_ref_count() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    UPDATE parked_pieces
    SET ref_count = ref_count + 1
    WHERE id = NEW.piece_id;
    RETURN NEW;
END;
$$;


--
-- Name: update_pdp_data_set_creates(); Type: FUNCTION; Schema: curio; Owner: -
--

CREATE FUNCTION update_pdp_data_set_creates() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.tx_status = 'pending' AND (NEW.tx_status = 'confirmed' OR NEW.tx_status = 'failed') THEN
        UPDATE pdp_data_set_creates
        SET ok = CASE
                     WHEN NEW.tx_status = 'failed' OR NEW.tx_success = FALSE THEN FALSE
                     WHEN NEW.tx_status = 'confirmed' AND NEW.tx_success = TRUE THEN TRUE
                     ELSE ok
            END
        WHERE create_message_hash = NEW.signed_tx_hash AND data_set_created = FALSE;
    END IF;
    RETURN NEW;
END;
$$;


--
-- Name: update_pdp_data_set_piece_adds(); Type: FUNCTION; Schema: curio; Owner: -
--

CREATE FUNCTION update_pdp_data_set_piece_adds() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.tx_status = 'pending' AND (NEW.tx_status = 'confirmed' OR NEW.tx_status = 'failed') THEN
        UPDATE pdp_data_set_piece_adds
        SET add_message_ok = CASE
                                WHEN NEW.tx_status = 'failed' OR NEW.tx_success = FALSE THEN FALSE
                                WHEN NEW.tx_status = 'confirmed' AND NEW.tx_success = TRUE THEN TRUE
                                ELSE add_message_ok
                            END
        WHERE add_message_hash = NEW.signed_tx_hash;
    END IF;
    RETURN NEW;
END;
$$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: eth_keys; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE eth_keys (
    address text NOT NULL,
    private_key bytea NOT NULL,
    role text NOT NULL
);


--
-- Name: harmony_config; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE harmony_config (
    id integer NOT NULL,
    title character varying(300) NOT NULL,
    config text NOT NULL,
    "timestamp" timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: harmony_config_history; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE harmony_config_history (
    id integer NOT NULL,
    title character varying(300) NOT NULL,
    config text NOT NULL,
    changed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: harmony_config_history_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE harmony_config_history_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: harmony_config_history_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE harmony_config_history_id_seq OWNED BY harmony_config_history.id;


--
-- Name: harmony_config_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE harmony_config_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: harmony_config_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE harmony_config_id_seq OWNED BY harmony_config.id;


--
-- Name: harmony_machine_details; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE harmony_machine_details (
    id integer NOT NULL,
    tasks text,
    layers text,
    startup_time timestamp with time zone,
    miners text,
    machine_id integer,
    machine_name text,
    version text
);


--
-- Name: harmony_machine_details_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE harmony_machine_details_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: harmony_machine_details_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE harmony_machine_details_id_seq OWNED BY harmony_machine_details.id;


--
-- Name: harmony_machines; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE harmony_machines (
    id integer NOT NULL,
    last_contact timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    host_and_port character varying(300) NOT NULL,
    cpu integer NOT NULL,
    ram bigint NOT NULL,
    gpu double precision NOT NULL,
    unschedulable boolean DEFAULT false,
    restart_request timestamp with time zone
);


--
-- Name: harmony_machines_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE harmony_machines_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: harmony_machines_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE harmony_machines_id_seq OWNED BY harmony_machines.id;


--
-- Name: harmony_task; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE harmony_task (
    id integer NOT NULL,
    initiated_by integer,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    posted_time timestamp with time zone NOT NULL,
    owner_id integer,
    added_by integer NOT NULL,
    previous_task integer,
    name character varying(16) NOT NULL,
    retries bigint DEFAULT 0 NOT NULL
);


--
-- Name: COLUMN harmony_task.initiated_by; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN harmony_task.initiated_by IS 'The task ID whose completion occasioned this task.';


--
-- Name: COLUMN harmony_task.update_time; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN harmony_task.update_time IS 'When it was last modified. not a heartbeat';


--
-- Name: COLUMN harmony_task.owner_id; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN harmony_task.owner_id IS 'may be null if between owners or not yet taken';


--
-- Name: COLUMN harmony_task.name; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN harmony_task.name IS 'The name of the task type.';


--
-- Name: harmony_task_follow; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE harmony_task_follow (
    id integer NOT NULL,
    owner_id integer NOT NULL,
    to_type character varying(16) NOT NULL,
    from_type character varying(16) NOT NULL
);


--
-- Name: harmony_task_follow_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE harmony_task_follow_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: harmony_task_follow_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE harmony_task_follow_id_seq OWNED BY harmony_task_follow.id;


--
-- Name: harmony_task_history; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE harmony_task_history (
    id integer NOT NULL,
    task_id integer NOT NULL,
    name character varying(16) NOT NULL,
    posted timestamp with time zone NOT NULL,
    work_start timestamp with time zone NOT NULL,
    work_end timestamp with time zone NOT NULL,
    result boolean NOT NULL,
    err character varying,
    completed_by_host_and_port character varying(300) NOT NULL
);


--
-- Name: COLUMN harmony_task_history.result; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN harmony_task_history.result IS 'Use to detemine if this was a successful run.';


--
-- Name: harmony_task_history_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE harmony_task_history_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: harmony_task_history_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE harmony_task_history_id_seq OWNED BY harmony_task_history.id;


--
-- Name: harmony_task_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE harmony_task_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: harmony_task_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE harmony_task_id_seq OWNED BY harmony_task.id;


--
-- Name: harmony_task_impl; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE harmony_task_impl (
    id integer NOT NULL,
    owner_id integer NOT NULL,
    name character varying(16) NOT NULL
);


--
-- Name: harmony_task_impl_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE harmony_task_impl_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: harmony_task_impl_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE harmony_task_impl_id_seq OWNED BY harmony_task_impl.id;


--
-- Name: harmony_task_singletons; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE harmony_task_singletons (
    task_name character varying(255) NOT NULL,
    task_id bigint,
    last_run_time timestamp with time zone,
    run_now_request boolean DEFAULT false NOT NULL
);


--
-- Name: harmony_test; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE harmony_test (
    task_id bigint NOT NULL,
    options text,
    result text
);


--
-- Name: message_send_eth_locks; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE message_send_eth_locks (
    from_address text NOT NULL,
    task_id bigint NOT NULL,
    claimed_at timestamp without time zone NOT NULL
);


--
-- Name: message_sends_eth; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE message_sends_eth (
    from_address text NOT NULL,
    to_address text NOT NULL,
    send_reason text NOT NULL,
    send_task_id integer NOT NULL,
    unsigned_tx bytea NOT NULL,
    unsigned_hash text NOT NULL,
    nonce bigint,
    signed_tx bytea,
    signed_hash text,
    send_time timestamp without time zone,
    send_success boolean,
    send_error text
);


--
-- Name: COLUMN message_sends_eth.from_address; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.from_address IS 'Ethereum 0x... address';


--
-- Name: COLUMN message_sends_eth.to_address; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.to_address IS 'Ethereum 0x... address';


--
-- Name: COLUMN message_sends_eth.send_reason; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.send_reason IS 'Optional description of send reason';


--
-- Name: COLUMN message_sends_eth.send_task_id; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.send_task_id IS 'Task ID of the send task';


--
-- Name: COLUMN message_sends_eth.unsigned_tx; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.unsigned_tx IS 'Unsigned transaction data';


--
-- Name: COLUMN message_sends_eth.unsigned_hash; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.unsigned_hash IS 'Hash of the unsigned transaction';


--
-- Name: COLUMN message_sends_eth.nonce; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.nonce IS 'Assigned transaction nonce, set while the send task is executing';


--
-- Name: COLUMN message_sends_eth.signed_tx; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.signed_tx IS 'Signed transaction data, set while the send task is executing';


--
-- Name: COLUMN message_sends_eth.signed_hash; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.signed_hash IS 'Hash of the signed transaction';


--
-- Name: COLUMN message_sends_eth.send_time; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.send_time IS 'Time when the send task was executed, set after pushing the transaction to the network';


--
-- Name: COLUMN message_sends_eth.send_success; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.send_success IS 'Whether this transaction was broadcasted to the network already, NULL if not yet attempted, TRUE if successful, FALSE if failed';


--
-- Name: COLUMN message_sends_eth.send_error; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN message_sends_eth.send_error IS 'Error message if send_success is FALSE';


--
-- Name: message_sends_eth_send_task_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE message_sends_eth_send_task_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: message_sends_eth_send_task_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE message_sends_eth_send_task_id_seq OWNED BY message_sends_eth.send_task_id;


--
-- Name: message_waits_eth; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE message_waits_eth (
    signed_tx_hash text NOT NULL,
    waiter_machine_id integer,
    confirmed_block_number bigint,
    confirmed_tx_hash text,
    confirmed_tx_data jsonb,
    tx_status text,
    tx_receipt jsonb,
    tx_success boolean
);


--
-- Name: parked_piece_refs; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE parked_piece_refs (
    ref_id bigint NOT NULL,
    piece_id bigint NOT NULL,
    data_url text,
    data_headers jsonb DEFAULT '{}'::jsonb NOT NULL,
    long_term boolean DEFAULT false NOT NULL
);


--
-- Name: parked_piece_refs_ref_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE parked_piece_refs_ref_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: parked_piece_refs_ref_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE parked_piece_refs_ref_id_seq OWNED BY parked_piece_refs.ref_id;


--
-- Name: parked_pieces; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE parked_pieces (
    id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    piece_cid text NOT NULL,
    piece_padded_size bigint NOT NULL,
    piece_raw_size bigint NOT NULL,
    complete boolean DEFAULT false NOT NULL,
    task_id bigint,
    cleanup_task_id bigint,
    long_term boolean DEFAULT false NOT NULL,
    skip boolean DEFAULT false NOT NULL,
    ref_count integer DEFAULT 0 NOT NULL
);


--
-- Name: parked_pieces_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE parked_pieces_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: parked_pieces_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE parked_pieces_id_seq OWNED BY parked_pieces.id;


--
-- Name: pdp_data_set; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_data_set (
    id bigint NOT NULL,
    client text NOT NULL,
    prev_challenge_request_epoch bigint,
    challenge_request_task_id bigint,
    challenge_request_msg_hash text,
    proving_period bigint,
    challenge_window bigint,
    prove_at_epoch bigint,
    init_ready boolean DEFAULT false NOT NULL,
    create_deal_id text NOT NULL,
    create_message_hash text NOT NULL,
    removed boolean DEFAULT false,
    remove_deal_id text,
    remove_message_hash text
);


--
-- Name: pdp_data_set_create; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_data_set_create (
    id text NOT NULL,
    client text NOT NULL,
    record_keeper text NOT NULL,
    extra_data bytea,
    task_id bigint,
    tx_hash text
);


--
-- Name: pdp_data_set_creates; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_data_set_creates (
    create_message_hash text NOT NULL,
    ok boolean,
    data_set_created boolean DEFAULT false NOT NULL,
    service text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: pdp_data_set_delete; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_data_set_delete (
    id text NOT NULL,
    client text NOT NULL,
    set_id bigint NOT NULL,
    extra_data bytea,
    task_id bigint,
    tx_hash text
);


--
-- Name: pdp_data_set_piece_adds; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_data_set_piece_adds (
    data_set bigint,
    piece text NOT NULL,
    add_message_hash text NOT NULL,
    add_message_ok boolean,
    add_message_index bigint NOT NULL,
    sub_piece text NOT NULL,
    sub_piece_offset bigint NOT NULL,
    sub_piece_size bigint NOT NULL,
    pdp_pieceref bigint NOT NULL,
    pieces_added boolean DEFAULT false NOT NULL
);


--
-- Name: pdp_data_set_pieces; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_data_set_pieces (
    data_set bigint NOT NULL,
    piece text NOT NULL,
    add_message_hash text NOT NULL,
    add_message_index bigint NOT NULL,
    piece_id bigint NOT NULL,
    sub_piece text NOT NULL,
    sub_piece_offset bigint NOT NULL,
    sub_piece_size bigint NOT NULL,
    pdp_pieceref bigint NOT NULL,
    rm_message_hash text,
    removed boolean DEFAULT false
);


--
-- Name: pdp_data_sets; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_data_sets (
    id bigint NOT NULL,
    prev_challenge_request_epoch bigint,
    challenge_request_task_id bigint,
    challenge_request_msg_hash text,
    proving_period bigint,
    challenge_window bigint,
    prove_at_epoch bigint,
    init_ready boolean DEFAULT false NOT NULL,
    create_message_hash text NOT NULL,
    service text NOT NULL,
    unrecoverable_proving_failure_epoch bigint,
    consecutive_prove_failures integer DEFAULT 0 NOT NULL,
    next_prove_attempt_at bigint
);


--
-- Name: COLUMN pdp_data_sets.unrecoverable_proving_failure_epoch; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN pdp_data_sets.unrecoverable_proving_failure_epoch IS 'Block height at which an unrecoverable proving failure was detected; NULL if active';


--
-- Name: COLUMN pdp_data_sets.consecutive_prove_failures; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN pdp_data_sets.consecutive_prove_failures IS 'Number of consecutive proving failures (resets on success)';


--
-- Name: COLUMN pdp_data_sets.next_prove_attempt_at; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN pdp_data_sets.next_prove_attempt_at IS 'Block height before which proving should not be attempted (backoff)';


--
-- Name: pdp_dataset_piece; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_dataset_piece (
    data_set_id bigint NOT NULL,
    client text NOT NULL,
    piece_cid_v2 text NOT NULL,
    piece bigint NOT NULL,
    piece_ref bigint NOT NULL,
    add_deal_id text NOT NULL,
    add_message_hash text NOT NULL,
    add_message_index bigint NOT NULL,
    removed boolean DEFAULT false,
    remove_deal_id text,
    remove_message_hash text,
    remove_message_index bigint
);


--
-- Name: pdp_delete_data_set; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_delete_data_set (
    id bigint NOT NULL,
    terminate_service_task_id bigint,
    after_terminate_service boolean DEFAULT false NOT NULL,
    terminate_tx_hash text,
    service_termination_epoch bigint,
    delete_data_set_task_id bigint,
    after_delete_data_set boolean DEFAULT false NOT NULL,
    delete_tx_hash text,
    terminated boolean DEFAULT false NOT NULL,
    deletion_allowed boolean DEFAULT false NOT NULL,
    client_requested_termination boolean DEFAULT false NOT NULL,
    termination_requested_at timestamp with time zone,
    termination_extra_data bytea,
    client_terminate_service_task_id bigint,
    cleanup_pieces_task_id bigint,
    cleanup_pieces_tx_hash text,
    CONSTRAINT pdp_delete_data_set_client_extra_data_check CHECK (((client_requested_termination = false) OR ((termination_extra_data IS NOT NULL) AND (octet_length(termination_extra_data) > 0))))
);


--
-- Name: pdp_ipni_task; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_ipni_task (
    context_id bytea NOT NULL,
    is_rm boolean NOT NULL,
    id text NOT NULL,
    provider text NOT NULL,
    created_at timestamp with time zone DEFAULT timezone('UTC'::text, now()) NOT NULL,
    task_id bigint,
    complete boolean DEFAULT false
);


--
-- Name: pdp_piece_delete; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_piece_delete (
    id text NOT NULL,
    client text NOT NULL,
    set_id bigint NOT NULL,
    pieces bigint[] NOT NULL,
    extra_data bytea,
    task_id bigint,
    tx_hash text
);


--
-- Name: pdp_piece_mh_to_commp; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_piece_mh_to_commp (
    mhash bytea NOT NULL,
    size bigint NOT NULL,
    commp text NOT NULL
);


--
-- Name: pdp_piece_pull_items; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_piece_pull_items (
    fetch_id bigint NOT NULL,
    piece_cid text NOT NULL,
    piece_raw_size bigint NOT NULL,
    source_url text NOT NULL,
    task_id bigint,
    failed boolean DEFAULT false NOT NULL,
    fail_reason text,
    complete boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    parked_piece_ref bigint,
    pull_parked_piece_id bigint,
    CONSTRAINT pdp_piece_pull_items_terminal_state_check CHECK ((NOT (complete AND failed)))
);


--
-- Name: COLUMN pdp_piece_pull_items.parked_piece_ref; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN pdp_piece_pull_items.parked_piece_ref IS 'Per-pull-item parked_piece_refs row; preserves the item source URL and is promoted into pdp_piecerefs on success.';


--
-- Name: COLUMN pdp_piece_pull_items.pull_parked_piece_id; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON COLUMN pdp_piece_pull_items.pull_parked_piece_id IS 'Set only when PullPiece created the parked_pieces row; ownership marker for retry/expiry cleanup, distinct from parked_piece_ref.piece_id for refs attached to rows owned by other flows.';


--
-- Name: pdp_piece_pulls; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_piece_pulls (
    id bigint NOT NULL,
    service text NOT NULL,
    extra_data_hash bytea NOT NULL,
    data_set_id bigint DEFAULT 0 NOT NULL,
    record_keeper text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now(),
    client_address text DEFAULT ''::text NOT NULL
);


--
-- Name: pdp_piece_pulls_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE pdp_piece_pulls_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: pdp_piece_pulls_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE pdp_piece_pulls_id_seq OWNED BY pdp_piece_pulls.id;


--
-- Name: pdp_piece_streaming_uploads; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_piece_streaming_uploads (
    id uuid NOT NULL,
    service text NOT NULL,
    piece_cid text,
    piece_size bigint,
    raw_size bigint,
    piece_ref bigint,
    created_at timestamp with time zone DEFAULT timezone('UTC'::text, now()) NOT NULL,
    complete boolean,
    completed_at timestamp with time zone
);


--
-- Name: pdp_piece_uploads; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_piece_uploads (
    id uuid NOT NULL,
    service text NOT NULL,
    check_hash_codec text NOT NULL,
    check_hash bytea NOT NULL,
    check_size bigint NOT NULL,
    piece_cid text,
    notify_url text NOT NULL,
    notify_task_id bigint,
    piece_ref bigint,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: pdp_piecerefs; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_piecerefs (
    id bigint NOT NULL,
    service text NOT NULL,
    piece_cid text NOT NULL,
    piece_ref bigint NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    data_set_refcount bigint DEFAULT 0 NOT NULL,
    indexing_task_id bigint,
    needs_indexing boolean DEFAULT false,
    ipni_task_id bigint,
    needs_ipni boolean DEFAULT false,
    needs_save_cache boolean DEFAULT true,
    save_cache_task_id bigint,
    caching_task_started timestamp with time zone,
    caching_task_completed timestamp with time zone,
    cached_proofgen_failure_count integer DEFAULT 0,
    indexed_at timestamp with time zone,
    advertisement_created_at timestamp with time zone
);


--
-- Name: pdp_piecerefs_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE pdp_piecerefs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: pdp_piecerefs_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE pdp_piecerefs_id_seq OWNED BY pdp_piecerefs.id;


--
-- Name: pdp_pipeline; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_pipeline (
    created_at timestamp with time zone DEFAULT timezone('UTC'::text, now()) NOT NULL,
    id text NOT NULL,
    client text NOT NULL,
    piece_cid_v2 text NOT NULL,
    data_set_id bigint NOT NULL,
    extra_data bytea,
    piece_ref bigint,
    downloaded boolean DEFAULT false,
    commp_task_id bigint,
    after_commp boolean DEFAULT false,
    deal_aggregation integer DEFAULT 0 NOT NULL,
    aggr_index bigint DEFAULT 0 NOT NULL,
    agg_task_id bigint,
    aggregated boolean DEFAULT false,
    add_piece_task_id bigint,
    after_add_piece boolean DEFAULT false,
    add_message_hash text,
    add_message_index bigint DEFAULT 0 NOT NULL,
    after_add_piece_msg boolean DEFAULT false,
    save_cache_task_id bigint,
    after_save_cache boolean DEFAULT false,
    indexing boolean DEFAULT false,
    indexing_created_at timestamp with time zone,
    indexing_task_id bigint,
    indexed boolean DEFAULT false,
    announce boolean DEFAULT false,
    announce_payload boolean DEFAULT false,
    announced boolean DEFAULT false,
    announced_payload boolean DEFAULT false,
    complete boolean DEFAULT false
);


--
-- Name: pdp_prove_tasks; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_prove_tasks (
    data_set bigint NOT NULL,
    task_id bigint NOT NULL
);


--
-- Name: pdp_proving_tasks; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_proving_tasks (
    data_set_id bigint NOT NULL,
    task_id bigint NOT NULL
);


--
-- Name: pdp_services; Type: TABLE; Schema: curio; Owner: -
--

CREATE TABLE pdp_services (
    id bigint NOT NULL,
    pubkey bytea NOT NULL,
    service_label text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: pdp_services_id_seq; Type: SEQUENCE; Schema: curio; Owner: -
--

CREATE SEQUENCE pdp_services_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: pdp_services_id_seq; Type: SEQUENCE OWNED BY; Schema: curio; Owner: -
--

ALTER SEQUENCE pdp_services_id_seq OWNED BY pdp_services.id;


--
-- Name: harmony_config id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_config ALTER COLUMN id SET DEFAULT nextval('harmony_config_id_seq'::regclass);


--
-- Name: harmony_config_history id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_config_history ALTER COLUMN id SET DEFAULT nextval('harmony_config_history_id_seq'::regclass);


--
-- Name: harmony_machine_details id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_machine_details ALTER COLUMN id SET DEFAULT nextval('harmony_machine_details_id_seq'::regclass);


--
-- Name: harmony_machines id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_machines ALTER COLUMN id SET DEFAULT nextval('harmony_machines_id_seq'::regclass);


--
-- Name: harmony_task id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task ALTER COLUMN id SET DEFAULT nextval('harmony_task_id_seq'::regclass);


--
-- Name: harmony_task_follow id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task_follow ALTER COLUMN id SET DEFAULT nextval('harmony_task_follow_id_seq'::regclass);


--
-- Name: harmony_task_history id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task_history ALTER COLUMN id SET DEFAULT nextval('harmony_task_history_id_seq'::regclass);


--
-- Name: harmony_task_impl id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task_impl ALTER COLUMN id SET DEFAULT nextval('harmony_task_impl_id_seq'::regclass);


--
-- Name: message_sends_eth send_task_id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY message_sends_eth ALTER COLUMN send_task_id SET DEFAULT nextval('message_sends_eth_send_task_id_seq'::regclass);


--
-- Name: parked_piece_refs ref_id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY parked_piece_refs ALTER COLUMN ref_id SET DEFAULT nextval('parked_piece_refs_ref_id_seq'::regclass);


--
-- Name: parked_pieces id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY parked_pieces ALTER COLUMN id SET DEFAULT nextval('parked_pieces_id_seq'::regclass);


--
-- Name: pdp_piece_pulls id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_pulls ALTER COLUMN id SET DEFAULT nextval('pdp_piece_pulls_id_seq'::regclass);


--
-- Name: pdp_piecerefs id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piecerefs ALTER COLUMN id SET DEFAULT nextval('pdp_piecerefs_id_seq'::regclass);


--
-- Name: pdp_services id; Type: DEFAULT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_services ALTER COLUMN id SET DEFAULT nextval('pdp_services_id_seq'::regclass);


--
-- Name: eth_keys eth_keys_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY eth_keys
    ADD CONSTRAINT eth_keys_pkey PRIMARY KEY (address);


--
-- Name: harmony_config_history harmony_config_history_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_config_history
    ADD CONSTRAINT harmony_config_history_pkey PRIMARY KEY (id);


--
-- Name: harmony_config harmony_config_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_config
    ADD CONSTRAINT harmony_config_pkey PRIMARY KEY (id);


--
-- Name: harmony_config harmony_config_title_key; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_config
    ADD CONSTRAINT harmony_config_title_key UNIQUE (title);


--
-- Name: harmony_machine_details harmony_machine_details_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_machine_details
    ADD CONSTRAINT harmony_machine_details_pkey PRIMARY KEY (id);


--
-- Name: harmony_machines harmony_machines_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_machines
    ADD CONSTRAINT harmony_machines_pkey PRIMARY KEY (id);


--
-- Name: harmony_task_follow harmony_task_follow_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task_follow
    ADD CONSTRAINT harmony_task_follow_pkey PRIMARY KEY (id);


--
-- Name: harmony_task_history harmony_task_history_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task_history
    ADD CONSTRAINT harmony_task_history_pkey PRIMARY KEY (id);


--
-- Name: harmony_task_impl harmony_task_impl_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task_impl
    ADD CONSTRAINT harmony_task_impl_pkey PRIMARY KEY (id);


--
-- Name: harmony_task harmony_task_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task
    ADD CONSTRAINT harmony_task_pkey PRIMARY KEY (id);


--
-- Name: harmony_task_singletons harmony_task_singletons_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task_singletons
    ADD CONSTRAINT harmony_task_singletons_pkey PRIMARY KEY (task_name);


--
-- Name: harmony_test harmony_test_pk; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_test
    ADD CONSTRAINT harmony_test_pk PRIMARY KEY (task_id);


--
-- Name: message_send_eth_locks message_send_eth_locks_pk; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY message_send_eth_locks
    ADD CONSTRAINT message_send_eth_locks_pk PRIMARY KEY (from_address);


--
-- Name: message_sends_eth message_sends_eth_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY message_sends_eth
    ADD CONSTRAINT message_sends_eth_pkey PRIMARY KEY (send_task_id);


--
-- Name: message_waits_eth message_waits_eth_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY message_waits_eth
    ADD CONSTRAINT message_waits_eth_pkey PRIMARY KEY (signed_tx_hash);


--
-- Name: parked_piece_refs parked_piece_refs_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY parked_piece_refs
    ADD CONSTRAINT parked_piece_refs_pkey PRIMARY KEY (ref_id);


--
-- Name: parked_pieces parked_pieces_piece_cid_cleanup_task_id_key; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY parked_pieces
    ADD CONSTRAINT parked_pieces_piece_cid_cleanup_task_id_key UNIQUE (piece_cid, piece_padded_size, long_term, cleanup_task_id);


--
-- Name: parked_pieces parked_pieces_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY parked_pieces
    ADD CONSTRAINT parked_pieces_pkey PRIMARY KEY (id);


--
-- Name: pdp_data_set pdp_data_set_create_deal_id_key; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set
    ADD CONSTRAINT pdp_data_set_create_deal_id_key UNIQUE (create_deal_id);


--
-- Name: pdp_data_set_create pdp_data_set_create_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_create
    ADD CONSTRAINT pdp_data_set_create_pkey PRIMARY KEY (id);


--
-- Name: pdp_data_set_delete pdp_data_set_delete_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_delete
    ADD CONSTRAINT pdp_data_set_delete_pkey PRIMARY KEY (id);


--
-- Name: pdp_data_set_piece_adds pdp_data_set_piece_adds_pk; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_piece_adds
    ADD CONSTRAINT pdp_data_set_piece_adds_pk PRIMARY KEY (add_message_hash, add_message_index);


--
-- Name: pdp_data_set_pieces pdp_data_set_pieces_piece_id_unique; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_pieces
    ADD CONSTRAINT pdp_data_set_pieces_piece_id_unique PRIMARY KEY (data_set, piece_id, sub_piece_offset);


--
-- Name: pdp_data_set pdp_data_set_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set
    ADD CONSTRAINT pdp_data_set_pkey PRIMARY KEY (id);


--
-- Name: pdp_data_set pdp_data_set_remove_deal_id_key; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set
    ADD CONSTRAINT pdp_data_set_remove_deal_id_key UNIQUE (remove_deal_id);


--
-- Name: pdp_dataset_piece pdp_dataset_piece_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_dataset_piece
    ADD CONSTRAINT pdp_dataset_piece_pkey PRIMARY KEY (data_set_id, piece);


--
-- Name: pdp_delete_data_set pdp_delete_data_set_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_delete_data_set
    ADD CONSTRAINT pdp_delete_data_set_pkey PRIMARY KEY (id);


--
-- Name: pdp_ipni_task pdp_ipni_task_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_ipni_task
    ADD CONSTRAINT pdp_ipni_task_pkey PRIMARY KEY (context_id, is_rm);


--
-- Name: pdp_piece_delete pdp_piece_delete_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_delete
    ADD CONSTRAINT pdp_piece_delete_pkey PRIMARY KEY (id);


--
-- Name: pdp_piece_mh_to_commp pdp_piece_mh_to_commp_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_mh_to_commp
    ADD CONSTRAINT pdp_piece_mh_to_commp_pkey PRIMARY KEY (mhash);


--
-- Name: pdp_piece_pull_items pdp_piece_pull_items_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_pull_items
    ADD CONSTRAINT pdp_piece_pull_items_pkey PRIMARY KEY (fetch_id, piece_cid, source_url);


--
-- Name: pdp_piece_pulls pdp_piece_pulls_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_pulls
    ADD CONSTRAINT pdp_piece_pulls_pkey PRIMARY KEY (id);


--
-- Name: pdp_piece_pulls pdp_piece_pulls_service_extra_data_hash_data_set_id_record__key; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_pulls
    ADD CONSTRAINT pdp_piece_pulls_service_extra_data_hash_data_set_id_record__key UNIQUE (service, extra_data_hash, data_set_id, record_keeper);


--
-- Name: pdp_piece_streaming_uploads pdp_piece_streaming_uploads_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_streaming_uploads
    ADD CONSTRAINT pdp_piece_streaming_uploads_pkey PRIMARY KEY (id);


--
-- Name: pdp_piece_uploads pdp_piece_uploads_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_uploads
    ADD CONSTRAINT pdp_piece_uploads_pkey PRIMARY KEY (id);


--
-- Name: pdp_piecerefs pdp_piecerefs_piece_ref_key; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piecerefs
    ADD CONSTRAINT pdp_piecerefs_piece_ref_key UNIQUE (piece_ref);


--
-- Name: pdp_piecerefs pdp_piecerefs_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piecerefs
    ADD CONSTRAINT pdp_piecerefs_pkey PRIMARY KEY (id);


--
-- Name: pdp_pipeline pdp_pipeline_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_pipeline
    ADD CONSTRAINT pdp_pipeline_pkey PRIMARY KEY (id, aggr_index);


--
-- Name: pdp_data_sets pdp_proof_sets_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_sets
    ADD CONSTRAINT pdp_proof_sets_pkey PRIMARY KEY (id);


--
-- Name: pdp_data_set_creates pdp_proofset_creates_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_creates
    ADD CONSTRAINT pdp_proofset_creates_pkey PRIMARY KEY (create_message_hash);


--
-- Name: pdp_prove_tasks pdp_prove_tasks_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_prove_tasks
    ADD CONSTRAINT pdp_prove_tasks_pkey PRIMARY KEY (data_set, task_id);


--
-- Name: pdp_proving_tasks pdp_proving_tasks_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_proving_tasks
    ADD CONSTRAINT pdp_proving_tasks_pkey PRIMARY KEY (data_set_id, task_id);


--
-- Name: pdp_services pdp_services_pkey; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_services
    ADD CONSTRAINT pdp_services_pkey PRIMARY KEY (id);


--
-- Name: pdp_services pdp_services_pubkey_key; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_services
    ADD CONSTRAINT pdp_services_pubkey_key UNIQUE (pubkey);


--
-- Name: pdp_services pdp_services_service_label_key; Type: CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_services
    ADD CONSTRAINT pdp_services_service_label_key UNIQUE (service_label);


--
-- Name: harmony_task_history_recent_task_result_idx; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX harmony_task_history_recent_task_result_idx ON harmony_task_history USING btree (work_end DESC, name, task_id, result);


--
-- Name: harmony_task_history_task_id_result_index; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX harmony_task_history_task_id_result_index ON harmony_task_history USING btree (task_id, result);


--
-- Name: harmony_task_history_work_index; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX harmony_task_history_work_index ON harmony_task_history USING btree (work_end DESC, completed_by_host_and_port, name, result);


--
-- Name: idx_config_history_title_time; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_config_history_title_time ON harmony_config_history USING btree (title, changed_at DESC);


--
-- Name: idx_harmony_machines_unschedulable; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_harmony_machines_unschedulable ON harmony_machines USING btree (unschedulable);


--
-- Name: idx_harmony_task_unowned_by_name; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_harmony_task_unowned_by_name ON harmony_task USING btree (name, update_time) WHERE (owner_id IS NULL);


--
-- Name: idx_message_sends_eth_nonce_lookup; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_message_sends_eth_nonce_lookup ON message_sends_eth USING btree (from_address, nonce DESC) WHERE (send_success = true);


--
-- Name: idx_message_sends_eth_reorg_check; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_message_sends_eth_reorg_check ON message_sends_eth USING btree (send_time, send_reason) WHERE ((send_success = true) AND (send_time IS NOT NULL) AND (signed_hash IS NOT NULL));


--
-- Name: idx_message_sends_eth_signed_hash_norm; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_message_sends_eth_signed_hash_norm ON message_sends_eth USING btree (lower(TRIM(BOTH FROM signed_hash))) WHERE ((send_success = true) AND (signed_hash IS NOT NULL));


--
-- Name: idx_message_waits_eth_pending; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_message_waits_eth_pending ON message_waits_eth USING btree (waiter_machine_id) WHERE ((waiter_machine_id IS NULL) AND (tx_status = 'pending'::text));


--
-- Name: idx_message_waits_eth_reorg_confirmed; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_message_waits_eth_reorg_confirmed ON message_waits_eth USING btree (confirmed_block_number, signed_tx_hash) WHERE ((tx_status = 'confirmed'::text) AND (tx_success = true) AND (confirmed_block_number IS NOT NULL));


--
-- Name: idx_message_waits_eth_waiter_pending; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_message_waits_eth_waiter_pending ON message_waits_eth USING btree (waiter_machine_id, signed_tx_hash) WHERE (tx_status = 'pending'::text);


--
-- Name: idx_parked_piece_refs_piece_id; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_parked_piece_refs_piece_id ON parked_piece_refs USING btree (piece_id);


--
-- Name: idx_parked_pieces_cleanup_eligible; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_parked_pieces_cleanup_eligible ON parked_pieces USING btree (ref_count, cleanup_task_id, id);


--
-- Name: idx_parked_pieces_incomplete_fetch; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_parked_pieces_incomplete_fetch ON parked_pieces USING btree (long_term) WHERE ((complete = false) AND (task_id IS NULL));


--
-- Name: idx_pdp_data_set_piece_adds_pdp_pieceref; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_data_set_piece_adds_pdp_pieceref ON pdp_data_set_piece_adds USING btree (pdp_pieceref);


--
-- Name: idx_pdp_data_set_piece_adds_pieces_added; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_data_set_piece_adds_pieces_added ON pdp_data_set_piece_adds USING btree (pieces_added);


--
-- Name: idx_pdp_data_set_piece_adds_unprocessed; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_data_set_piece_adds_unprocessed ON pdp_data_set_piece_adds USING btree (data_set, add_message_hash) WHERE ((add_message_ok = true) AND (pieces_added = false));


--
-- Name: idx_pdp_data_set_pieces_pdp_pieceref; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_data_set_pieces_pdp_pieceref ON pdp_data_set_pieces USING btree (pdp_pieceref);


--
-- Name: idx_pdp_data_set_pieces_pending_removal; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_data_set_pieces_pending_removal ON pdp_data_set_pieces USING btree (data_set, piece_id) WHERE ((rm_message_hash IS NOT NULL) AND (removed = false));


--
-- Name: idx_pdp_dds_cleanup_task; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_dds_cleanup_task ON pdp_delete_data_set USING btree (cleanup_pieces_task_id);


--
-- Name: idx_pdp_dds_client_term_task; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_dds_client_term_task ON pdp_delete_data_set USING btree (client_terminate_service_task_id);


--
-- Name: idx_pdp_dds_delete_task; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_dds_delete_task ON pdp_delete_data_set USING btree (delete_data_set_task_id);


--
-- Name: idx_pdp_dds_term_task; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_dds_term_task ON pdp_delete_data_set USING btree (terminate_service_task_id);


--
-- Name: idx_pdp_piece_pull_items_parked_piece_ref; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_piece_pull_items_parked_piece_ref ON pdp_piece_pull_items USING btree (parked_piece_ref);


--
-- Name: idx_pdp_piece_pull_items_pull_parked_piece_id; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_piece_pull_items_pull_parked_piece_id ON pdp_piece_pull_items USING btree (pull_parked_piece_id);


--
-- Name: idx_pdp_piece_pulls_created_at; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_piece_pulls_created_at ON pdp_piece_pulls USING btree (created_at);


--
-- Name: idx_pdp_piece_uploads_notify_pending; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_piece_uploads_notify_pending ON pdp_piece_uploads USING btree (piece_ref) WHERE ((notify_task_id IS NULL) AND (piece_ref IS NOT NULL));


--
-- Name: idx_pdp_piecerefs_indexing_pending; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_piecerefs_indexing_pending ON pdp_piecerefs USING btree (created_at) WHERE ((indexing_task_id IS NULL) AND (needs_indexing = true));


--
-- Name: idx_pdp_piecerefs_ipni_pending; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_piecerefs_ipni_pending ON pdp_piecerefs USING btree (created_at) WHERE ((ipni_task_id IS NULL) AND (needs_ipni = true));


--
-- Name: idx_pdp_piecerefs_zero_refcount_created; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX idx_pdp_piecerefs_zero_refcount_created ON pdp_piecerefs USING btree (created_at, id) WHERE (data_set_refcount = 0);


--
-- Name: machine_details_machine_id; Type: INDEX; Schema: curio; Owner: -
--

CREATE UNIQUE INDEX machine_details_machine_id ON harmony_machine_details USING btree (machine_id);


--
-- Name: message_sends_eth_success_index; Type: INDEX; Schema: curio; Owner: -
--

CREATE UNIQUE INDEX message_sends_eth_success_index ON message_sends_eth USING btree (from_address, nonce) WHERE (send_success IS NOT FALSE);


--
-- Name: INDEX message_sends_eth_success_index; Type: COMMENT; Schema: curio; Owner: -
--

COMMENT ON INDEX message_sends_eth_success_index IS 'message_sends_eth_success_index enforces sender/nonce uniqueness, it is a conditional index that only indexes rows where send_success is not false. This allows us to have multiple rows with the same sender/nonce, as long as only one of them was successfully broadcasted (true) to the network or is in the process of being broadcasted (null).';


--
-- Name: parked_pieces_active_piece_key; Type: INDEX; Schema: curio; Owner: -
--

CREATE UNIQUE INDEX parked_pieces_active_piece_key ON parked_pieces USING btree (piece_cid, piece_padded_size, long_term) WHERE (cleanup_task_id IS NULL);


--
-- Name: pdp_piecerefs_indexing_task_id; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX pdp_piecerefs_indexing_task_id ON pdp_piecerefs USING btree (indexing_task_id) WHERE (indexing_task_id IS NOT NULL);


--
-- Name: pdp_piecerefs_ipni_task_id; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX pdp_piecerefs_ipni_task_id ON pdp_piecerefs USING btree (ipni_task_id) WHERE (ipni_task_id IS NOT NULL);


--
-- Name: pdp_piecerefs_piece_cid_idx; Type: INDEX; Schema: curio; Owner: -
--

CREATE INDEX pdp_piecerefs_piece_cid_idx ON pdp_piecerefs USING btree (piece_cid);


--
-- Name: parked_piece_refs parked_piece_refs_delete_refcount; Type: TRIGGER; Schema: curio; Owner: -
--

CREATE TRIGGER parked_piece_refs_delete_refcount AFTER DELETE ON parked_piece_refs FOR EACH ROW EXECUTE FUNCTION decrement_parked_piece_ref_count();


--
-- Name: parked_piece_refs parked_piece_refs_insert_refcount; Type: TRIGGER; Schema: curio; Owner: -
--

CREATE TRIGGER parked_piece_refs_insert_refcount AFTER INSERT ON parked_piece_refs FOR EACH ROW EXECUTE FUNCTION increment_parked_piece_ref_count();


--
-- Name: parked_piece_refs parked_piece_refs_update_refcount; Type: TRIGGER; Schema: curio; Owner: -
--

CREATE TRIGGER parked_piece_refs_update_refcount AFTER UPDATE ON parked_piece_refs FOR EACH ROW EXECUTE FUNCTION adjust_parked_piece_ref_count_on_update();


--
-- Name: message_waits_eth pdp_data_set_add_message_status_change; Type: TRIGGER; Schema: curio; Owner: -
--

CREATE TRIGGER pdp_data_set_add_message_status_change AFTER UPDATE OF tx_status, tx_success ON message_waits_eth FOR EACH ROW EXECUTE FUNCTION update_pdp_data_set_piece_adds();


--
-- Name: message_waits_eth pdp_data_set_create_message_status_change; Type: TRIGGER; Schema: curio; Owner: -
--

CREATE TRIGGER pdp_data_set_create_message_status_change AFTER UPDATE OF tx_status, tx_success ON message_waits_eth FOR EACH ROW EXECUTE FUNCTION update_pdp_data_set_creates();


--
-- Name: pdp_data_set_pieces pdp_data_set_piece_delete; Type: TRIGGER; Schema: curio; Owner: -
--

CREATE TRIGGER pdp_data_set_piece_delete AFTER DELETE ON pdp_data_set_pieces FOR EACH ROW WHEN ((old.pdp_pieceref IS NOT NULL)) EXECUTE FUNCTION decrement_data_set_refcount();


--
-- Name: pdp_data_set_pieces pdp_data_set_piece_insert; Type: TRIGGER; Schema: curio; Owner: -
--

CREATE TRIGGER pdp_data_set_piece_insert AFTER INSERT ON pdp_data_set_pieces FOR EACH ROW WHEN ((new.pdp_pieceref IS NOT NULL)) EXECUTE FUNCTION increment_data_set_refcount();


--
-- Name: pdp_data_set_pieces pdp_data_set_piece_update; Type: TRIGGER; Schema: curio; Owner: -
--

CREATE TRIGGER pdp_data_set_piece_update AFTER UPDATE ON pdp_data_set_pieces FOR EACH ROW EXECUTE FUNCTION adjust_data_set_refcount_on_update();


--
-- Name: harmony_machine_details harmony_machine_details_machine_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_machine_details
    ADD CONSTRAINT harmony_machine_details_machine_id_fkey FOREIGN KEY (machine_id) REFERENCES harmony_machines(id) ON DELETE CASCADE;


--
-- Name: harmony_task_follow harmony_task_follow_owner_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task_follow
    ADD CONSTRAINT harmony_task_follow_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES harmony_machines(id) ON DELETE CASCADE;


--
-- Name: harmony_task_impl harmony_task_impl_owner_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task_impl
    ADD CONSTRAINT harmony_task_impl_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES harmony_machines(id) ON DELETE CASCADE;


--
-- Name: harmony_task harmony_task_owner_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task
    ADD CONSTRAINT harmony_task_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES harmony_machines(id) ON DELETE SET NULL;


--
-- Name: harmony_task_singletons harmony_task_singletons_task_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY harmony_task_singletons
    ADD CONSTRAINT harmony_task_singletons_task_id_fkey FOREIGN KEY (task_id) REFERENCES harmony_task(id) ON DELETE SET NULL;


--
-- Name: message_waits_eth message_waits_eth_waiter_machine_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY message_waits_eth
    ADD CONSTRAINT message_waits_eth_waiter_machine_id_fkey FOREIGN KEY (waiter_machine_id) REFERENCES harmony_machines(id) ON DELETE SET NULL;


--
-- Name: parked_piece_refs parked_piece_refs_piece_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY parked_piece_refs
    ADD CONSTRAINT parked_piece_refs_piece_id_fkey FOREIGN KEY (piece_id) REFERENCES parked_pieces(id) ON DELETE CASCADE;


--
-- Name: pdp_data_set pdp_data_set_challenge_request_task_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set
    ADD CONSTRAINT pdp_data_set_challenge_request_task_id_fkey FOREIGN KEY (challenge_request_task_id) REFERENCES harmony_task(id) ON DELETE SET NULL;


--
-- Name: pdp_data_set_piece_adds pdp_data_set_piece_adds_pdp_pieceref_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_piece_adds
    ADD CONSTRAINT pdp_data_set_piece_adds_pdp_pieceref_fkey FOREIGN KEY (pdp_pieceref) REFERENCES pdp_piecerefs(id) ON DELETE CASCADE;


--
-- Name: pdp_data_set_pieces pdp_data_set_pieces_pdp_pieceref_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_pieces
    ADD CONSTRAINT pdp_data_set_pieces_pdp_pieceref_fkey FOREIGN KEY (pdp_pieceref) REFERENCES pdp_piecerefs(id) ON DELETE CASCADE;


--
-- Name: pdp_delete_data_set pdp_delete_data_set_cleanup_task_fk; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_delete_data_set
    ADD CONSTRAINT pdp_delete_data_set_cleanup_task_fk FOREIGN KEY (cleanup_pieces_task_id) REFERENCES harmony_task(id) ON DELETE SET NULL;


--
-- Name: pdp_delete_data_set pdp_delete_data_set_client_terminate_task_fk; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_delete_data_set
    ADD CONSTRAINT pdp_delete_data_set_client_terminate_task_fk FOREIGN KEY (client_terminate_service_task_id) REFERENCES harmony_task(id) ON DELETE CASCADE;


--
-- Name: pdp_delete_data_set pdp_delete_data_set_delete_task_fk; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_delete_data_set
    ADD CONSTRAINT pdp_delete_data_set_delete_task_fk FOREIGN KEY (delete_data_set_task_id) REFERENCES harmony_task(id) ON DELETE SET NULL;


--
-- Name: pdp_delete_data_set pdp_delete_data_set_terminate_task_fk; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_delete_data_set
    ADD CONSTRAINT pdp_delete_data_set_terminate_task_fk FOREIGN KEY (terminate_service_task_id) REFERENCES harmony_task(id) ON DELETE SET NULL;


--
-- Name: pdp_piece_pull_items pdp_piece_pull_items_fetch_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_pull_items
    ADD CONSTRAINT pdp_piece_pull_items_fetch_id_fkey FOREIGN KEY (fetch_id) REFERENCES pdp_piece_pulls(id) ON DELETE CASCADE;


--
-- Name: pdp_piece_pull_items pdp_piece_pull_items_parked_piece_ref_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_pull_items
    ADD CONSTRAINT pdp_piece_pull_items_parked_piece_ref_fkey FOREIGN KEY (parked_piece_ref) REFERENCES parked_piece_refs(ref_id) ON DELETE SET NULL;


--
-- Name: pdp_piece_pull_items pdp_piece_pull_items_pull_parked_piece_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_pull_items
    ADD CONSTRAINT pdp_piece_pull_items_pull_parked_piece_id_fkey FOREIGN KEY (pull_parked_piece_id) REFERENCES parked_pieces(id) ON DELETE SET NULL;


--
-- Name: pdp_piece_pull_items pdp_piece_pull_items_task_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_pull_items
    ADD CONSTRAINT pdp_piece_pull_items_task_id_fkey FOREIGN KEY (task_id) REFERENCES harmony_task(id) ON DELETE SET NULL;


--
-- Name: pdp_piece_pulls pdp_piece_pulls_service_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_pulls
    ADD CONSTRAINT pdp_piece_pulls_service_fkey FOREIGN KEY (service) REFERENCES pdp_services(service_label) ON DELETE CASCADE;


--
-- Name: pdp_piece_uploads pdp_piece_uploads_piece_ref_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_uploads
    ADD CONSTRAINT pdp_piece_uploads_piece_ref_fkey FOREIGN KEY (piece_ref) REFERENCES parked_piece_refs(ref_id) ON DELETE SET NULL;


--
-- Name: pdp_piece_uploads pdp_piece_uploads_service_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piece_uploads
    ADD CONSTRAINT pdp_piece_uploads_service_fkey FOREIGN KEY (service) REFERENCES pdp_services(service_label) ON DELETE CASCADE;


--
-- Name: pdp_piecerefs pdp_piecerefs_piece_ref_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piecerefs
    ADD CONSTRAINT pdp_piecerefs_piece_ref_fkey FOREIGN KEY (piece_ref) REFERENCES parked_piece_refs(ref_id) ON DELETE CASCADE;


--
-- Name: pdp_piecerefs pdp_piecerefs_service_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_piecerefs
    ADD CONSTRAINT pdp_piecerefs_service_fkey FOREIGN KEY (service) REFERENCES pdp_services(service_label) ON DELETE CASCADE;


--
-- Name: pdp_data_sets pdp_proof_sets_challenge_request_task_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_sets
    ADD CONSTRAINT pdp_proof_sets_challenge_request_task_id_fkey FOREIGN KEY (challenge_request_task_id) REFERENCES harmony_task(id) ON DELETE SET NULL;


--
-- Name: pdp_data_sets pdp_proof_sets_service_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_sets
    ADD CONSTRAINT pdp_proof_sets_service_fkey FOREIGN KEY (service) REFERENCES pdp_services(service_label) ON DELETE RESTRICT;


--
-- Name: pdp_data_set_creates pdp_proofset_creates_create_message_hash_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_creates
    ADD CONSTRAINT pdp_proofset_creates_create_message_hash_fkey FOREIGN KEY (create_message_hash) REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE;


--
-- Name: pdp_data_set_creates pdp_proofset_creates_service_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_creates
    ADD CONSTRAINT pdp_proofset_creates_service_fkey FOREIGN KEY (service) REFERENCES pdp_services(service_label) ON DELETE CASCADE;


--
-- Name: pdp_data_set_piece_adds pdp_proofset_root_adds_add_message_hash_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_piece_adds
    ADD CONSTRAINT pdp_proofset_root_adds_add_message_hash_fkey FOREIGN KEY (add_message_hash) REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE;


--
-- Name: pdp_data_set_piece_adds pdp_proofset_root_adds_proofset_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_piece_adds
    ADD CONSTRAINT pdp_proofset_root_adds_proofset_fkey FOREIGN KEY (data_set) REFERENCES pdp_data_sets(id) ON DELETE CASCADE;


--
-- Name: pdp_data_set_pieces pdp_proofset_roots_add_message_hash_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_pieces
    ADD CONSTRAINT pdp_proofset_roots_add_message_hash_fkey FOREIGN KEY (add_message_hash) REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE;


--
-- Name: pdp_data_set_pieces pdp_proofset_roots_proofset_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_data_set_pieces
    ADD CONSTRAINT pdp_proofset_roots_proofset_fkey FOREIGN KEY (data_set) REFERENCES pdp_data_sets(id) ON DELETE CASCADE;


--
-- Name: pdp_prove_tasks pdp_prove_tasks_proofset_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_prove_tasks
    ADD CONSTRAINT pdp_prove_tasks_proofset_fkey FOREIGN KEY (data_set) REFERENCES pdp_data_sets(id) ON DELETE CASCADE;


--
-- Name: pdp_prove_tasks pdp_prove_tasks_task_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_prove_tasks
    ADD CONSTRAINT pdp_prove_tasks_task_id_fkey FOREIGN KEY (task_id) REFERENCES harmony_task(id) ON DELETE CASCADE;


--
-- Name: pdp_proving_tasks pdp_proving_tasks_data_set_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_proving_tasks
    ADD CONSTRAINT pdp_proving_tasks_data_set_id_fkey FOREIGN KEY (data_set_id) REFERENCES pdp_data_set(id) ON DELETE CASCADE;


--
-- Name: pdp_proving_tasks pdp_proving_tasks_task_id_fkey; Type: FK CONSTRAINT; Schema: curio; Owner: -
--

ALTER TABLE ONLY pdp_proving_tasks
    ADD CONSTRAINT pdp_proving_tasks_task_id_fkey FOREIGN KEY (task_id) REFERENCES harmony_task(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


