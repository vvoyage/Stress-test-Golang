-- noinspection SqlUnreachableForFile

-- Сначала создаем расширение uuid-ossp
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Создаем все таблицы
CREATE TABLE public.systems (
                                system_id varchar(100) NOT NULL,
                                description varchar(100),
                                addr varchar(100),
                                resource varchar(100),
                                active boolean DEFAULT false NOT NULL,
                                predefined boolean DEFAULT false
)
    WITH (oids = false);

CREATE TABLE public.channels (
                                 channel varchar(36) NOT NULL,
                                 system_id varchar(100),
                                 channel_description varchar(100),
                                 enable_route boolean DEFAULT true,
                                 predefined boolean DEFAULT false
)
    WITH (oids = false);

CREATE TABLE public.messages (
                                 message_id uuid DEFAULT uuid_generate_v4() NOT NULL,
                                 message_created timestamp(6) without time zone DEFAULT now(),
                                 message_updated timestamp(6) without time zone DEFAULT now(),
                                 src_system varchar(100),
                                 dst_system varchar(100),
                                 data_type varchar(36),
                                 data_format varchar(20),
                                 ids varchar(130),
                                 ver_id varchar(50),
                                 ver_no varchar(20),
                                 envelope text DEFAULT ''::text,
                                 body text,
                                 original_id uuid,
                                 destination_channel varchar(36) DEFAULT ''::character varying,
                                 hold_period timestamp(6) without time zone,
                                 try_count integer DEFAULT 0,
                                 last_error text DEFAULT ''::text
)
    WITH (oids = false);

CREATE TABLE public.queues (
                               queue_id serial NOT NULL,
                               channel_id varchar(36) NOT NULL,
                               message_id uuid NOT NULL,
                               queued timestamp without time zone NOT NULL
)
    WITH (oids = false);

CREATE TABLE public.data_types (
                                   data_type varchar(36) NOT NULL,
                                   data_type_description varchar(255),
                                   xml_schema text DEFAULT ''::text,
                                   target_type text DEFAULT ''::text,
                                   target_namespace text DEFAULT ''::text
)
    WITH (oids = false);

CREATE TABLE public.routes (
                               route_id serial NOT NULL,
                               route_caption varchar(100)
)
    WITH (oids = false);

CREATE TABLE public.bindings (
                                 route_id integer NOT NULL,
                                 data_type varchar(36) NOT NULL,
                                 src_system varchar(36) DEFAULT ''::character varying NOT NULL,
                                 dst_system varchar(36) DEFAULT ''::character varying NOT NULL
)
    WITH (oids = false);

CREATE TABLE public.systems_mapping (
                                        system_id varchar(100) NOT NULL,
                                        alias varchar(100) NOT NULL
)
    WITH (oids = false);

CREATE TABLE public.resend (
                               data_type varchar(100) NOT NULL,
                               channel_id varchar(100) DEFAULT '%'::character varying NOT NULL,
                               error_mask varchar(256) NOT NULL,
                               max_try integer DEFAULT 10,
                               delay integer DEFAULT 300,
                               description varchar(100) DEFAULT ''::character varying
)
    WITH (oids = false);

CREATE TABLE public.nested_schemes (
                                       data_type varchar(100) NOT NULL,
                                       inherit_type varchar(100) NOT NULL
)
    WITH (oids = false);

-- Теперь создаем функции и процедуры

-- Изменяем CREATE OR ALTER на CREATE OR REPLACE
CREATE OR REPLACE FUNCTION public.subscribers (
  id uuid
)
RETURNS SETOF varchar
AS
$body$
with mapping as (
            select map.system_id from systems_mapping map
            inner join messages mm on mm.dst_system=map.alias
            where mm.message_id = id
        )
select distinct c.channel from messages m
                                   inner join bindings b on m.data_type like b.data_type || '%'
    and m.src_system like b.src_system || '%'
                                   inner join mapping map on map.system_id like b.dst_system ||'%'
                                   inner join channels c on map.system_id = c.system_id and c.enable_route
where m.message_id = id and m.dst_system <> ''
union all
select distinct c.channel from messages m
                                   inner join bindings b on  m.data_type like b.data_type || '%'
    and m.src_system like b.src_system || '%'
                                   inner join channels c on c.system_id like b.dst_system || '%'
where m.message_id = id and m.dst_system = '' and c.enable_route
    $body$
LANGUAGE sql;

CREATE OR REPLACE FUNCTION public.clone_message (
  id uuid
)
RETURNS uuid
AS
$body$
insert into messages(src_system, dst_system, data_type, data_format, ids, ver_id, ver_no, body, original_id)
select src_system, dst_system, data_type, data_format, ids, ver_id, ver_no, body, id
from messages where message_id = id
    returning message_id
$body$
LANGUAGE sql;

CREATE OR REPLACE FUNCTION public.valid_consumer (
  consumer character varying
)
RETURNS bigint
AS
$body$
select count(c.channel)
from channels c
         inner join systems s on c.system_id = s.system_id
where c.channel = consumer    and c.enable_route and s.active and s.resource is not null
    $body$
LANGUAGE sql;

CREATE OR REPLACE PROCEDURE public.resend_message (
  qid bigint,
  mid uuid
)
AS
$body$
declare new_channel VARCHAR(50);
BEGIN
--    set new_channel = '';
select into new_channel case
        when m.destination_channel ='' or m.destination_channel=''
        then 'sys:esb'
        else m.destination_channel
end as new_channel
    from messages m inner join queues q on m.message_id = q.message_id and
    m.message_id = mid and q.queue_id = qid and q.channel_id = 'sys:dlq';
update queues  set channel_id = new_channel where queue_id = qid and
    new_channel is not null and new_channel<>'';
END
$body$
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION public.change_queue (
  qfrom character varying,
  qto character varying,
  cnt integer
)
RETURNS integer
AS
$body$
begin
UPDATE queues SET channel_id= qTo where queue_id in
                                        (select queue_id from queues WHERE channel_id = qFrom order by queue_id LIMIT cnt);
return cnt ;
commit;
end ;
$body$
LANGUAGE plpgsql;

-- Пропускаем определение gen_random_uuid, так как он уже определен в uuid-ossp
-- CREATE FUNCTION public.gen_random_uuid ()...

CREATE OR REPLACE PROCEDURE public.delete_message (
  qid bigint,
  mid uuid
)
LANGUAGE 'sql'
SECURITY INVOKER
BEGIN ATOMIC
DELETE FROM queues
WHERE ((queues.queue_id = delete_message.qid) AND (queues.message_id = delete_message.mid));
DELETE FROM messages
WHERE (messages.message_id = delete_message.mid);
END;

-- Индексы и ограничения (можно добавить после создания всех таблиц)
CREATE INDEX channels_idx ON public.channels USING btree (system_id);
CREATE INDEX queues_idx ON public.queues USING btree (channel_id, queued);
CREATE INDEX bindings_idx ON public.bindings USING btree (data_type, src_system);

ALTER TABLE ONLY public.channels
    ADD CONSTRAINT channels_pkey
    PRIMARY KEY (channel);

ALTER TABLE ONLY public.data_types
    ADD CONSTRAINT data_types_pkey
    PRIMARY KEY (data_type);

ALTER TABLE ONLY public.routes
    ADD CONSTRAINT routes_pkey
    PRIMARY KEY (route_id);

ALTER TABLE ONLY public.systems
    ADD CONSTRAINT nodes_pkey
    PRIMARY KEY (system_id);

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_pkey
    PRIMARY KEY (message_id);

ALTER TABLE ONLY public.systems_mapping
    ADD CONSTRAINT systems_mapping_alias_key
    UNIQUE (alias);

ALTER TABLE ONLY public.queues
    ADD CONSTRAINT queues_pkey
    PRIMARY KEY (queue_id);

ALTER TABLE ONLY public.bindings
    ADD CONSTRAINT bindings_pkey
    PRIMARY KEY (route_id, data_type, src_system, dst_system);

ALTER TABLE ONLY public.resend
    ADD CONSTRAINT resend_pkey
    PRIMARY KEY (data_type, channel_id, error_mask);

ALTER TABLE ONLY public.nested_schemes
    ADD CONSTRAINT nested_schemes_pkey
    PRIMARY KEY (data_type, inherit_type);

-- Комментарии
COMMENT ON SCHEMA public IS 'Шина данных';
COMMENT ON TABLE public.systems IS 'Системы';
COMMENT ON COLUMN public.systems.system_id IS 'идентификатор системы';
COMMENT ON COLUMN public.systems.description IS 'Описание системы';
COMMENT ON COLUMN public.systems.addr IS 'ключ доступа на отправку';
COMMENT ON TABLE public.channels IS 'Каналы (имена очередей)';
COMMENT ON TABLE public.data_types IS 'Типы данных';
COMMENT ON TABLE public.routes IS 'Описание маршрутов';
COMMENT ON TABLE public.bindings IS 'Шаблоны маршрутизации сообщений';
COMMENT ON TABLE public.systems_mapping IS 'Псевдонимы систем для сопоставления получателей';
COMMENT ON TABLE public.messages IS 'Сообщения. При клонировании генерируются копии';
COMMENT ON TABLE public.queues IS 'Очереди сообщений';
COMMENT ON TABLE public.resend IS 'правила автоповторов сообщений из dlq';
COMMENT ON COLUMN public.resend.data_type IS 'шаблон типа данных';
COMMENT ON COLUMN public.resend.channel_id IS 'шаблон канала';
COMMENT ON COLUMN public.resend.error_mask IS 'шаблон ошибки';
COMMENT ON COLUMN public.resend.max_try IS 'количество попыток';
COMMENT ON COLUMN public.resend.delay IS 'интервал повтора в секундах';

-- Назначение владельца для delete_message
ALTER PROCEDURE public.delete_message (qid bigint, mid uuid)
    OWNER TO postgres;