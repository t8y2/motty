import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import System from 'system';

if (ARGV.length !== 1) {
    printerr('usage: dbus-run-session -- gjs -m tray_registration_e2e.js /path/to/motty');
    System.exit(2);
}

const loop = GLib.MainLoop.new(null, false);
const connection = Gio.DBus.session;
const started = GLib.get_monotonic_time();
let child;
let propertiesReady = false;
let menuReady = false;
let registered = false;
let beforeWake = false;
let startupTimeout;

function elapsed() {
    return Math.round((GLib.get_monotonic_time() - started) / 1000);
}

function call(destination, path, iface, method, parameters, signature, done) {
    connection.call(destination, path, iface, method, parameters,
        GLib.VariantType.new(signature), Gio.DBusCallFlags.NONE, 5000, null,
        (source, result) => {
            try {
                done(source.call_finish(result).deep_unpack());
            } catch (error) {
                printerr(`${method}: ${error}`);
            }
        });
}

const xml = `<node><interface name="org.kde.StatusNotifierWatcher">
<method name="RegisterStatusNotifierItem"><arg type="s" direction="in"/></method>
</interface></node>`;
const implementation = Gio.DBusExportedObject.wrapJSObject(xml, {
    RegisterStatusNotifierItemAsync([name], invocation) {
        registered = true;
        GLib.source_remove(startupTimeout);
        print(`REGISTER ${elapsed()}ms ${name}`);
        call(name, '/StatusNotifierItem', 'org.freedesktop.DBus.Properties', 'GetAll',
            GLib.Variant.new('(s)', ['org.kde.StatusNotifierItem']), '(a{sv})', ([properties]) => {
                propertiesReady = properties.IconPixmap.n_children() > 0;
                print(`PROPERTIES ${elapsed()}ms ready=${propertiesReady}`);
            });
        call(name, '/MenuBar', 'com.canonical.dbusmenu', 'GetLayout',
            GLib.Variant.new('(iias)', [0, -1, ['type', 'children-display']]), '(u(ia{sv}av))', () => {
                call(name, '/MenuBar', 'com.canonical.dbusmenu', 'GetGroupProperties',
                    GLib.Variant.new('(aias)', [[], []]), '(a(ia{sv}))', ([items]) => {
                        const labels = items.flatMap(([, properties]) =>
                            properties.label ? [properties.label.deep_unpack()] : []);
                        menuReady = labels.some(label => label.length > 0);
                        print(`MENU ${elapsed()}ms ready=${menuReady} labels=${JSON.stringify(labels)}`);
                    });
            });
        GLib.timeout_add(GLib.PRIORITY_DEFAULT, 50, () => {
            invocation.return_value(GLib.Variant.new('()', []));
            print(`ACK ${elapsed()}ms`);
            return GLib.SOURCE_REMOVE;
        });
        GLib.timeout_add(GLib.PRIORITY_DEFAULT, 2500, () => {
            beforeWake = propertiesReady && menuReady;
            print(`BEFORE_EXTERNAL_WAKE registered=${registered} properties=${propertiesReady} menu=${menuReady}`);
            connection.call_sync('org.freedesktop.DBus', '/org/freedesktop/DBus',
                'org.freedesktop.DBus', 'RequestName',
                GLib.Variant.new('(su)', ['org.example.Issue692Wake', 4]),
                GLib.VariantType.new('(u)'), Gio.DBusCallFlags.NONE, 1000, null);
            return GLib.SOURCE_REMOVE;
        });
        GLib.timeout_add(GLib.PRIORITY_DEFAULT, 4500, () => {
            print(`RESULT beforeWake=${beforeWake} afterWake=${propertiesReady && menuReady}`);
            child.force_exit();
            child.wait(null);
            loop.quit();
            return GLib.SOURCE_REMOVE;
        });
    },
});
implementation.export(connection, '/StatusNotifierWatcher');
Gio.bus_own_name_on_connection(connection, 'org.kde.StatusNotifierWatcher',
    Gio.BusNameOwnerFlags.NONE, () => {
        child = Gio.Subprocess.new([ARGV[0]], Gio.SubprocessFlags.NONE);
        startupTimeout = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 20000, () => {
            printerr('the application did not register a tray item');
            child.force_exit();
            child.wait(null);
            loop.quit();
            return GLib.SOURCE_REMOVE;
        });
    }, () => {
        printerr('could not own the watcher name');
        loop.quit();
    });
loop.run();
System.exit(beforeWake && propertiesReady && menuReady ? 0 : 1);
