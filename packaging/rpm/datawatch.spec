Name:           datawatch
Version:        0.1.0
Release:        1%{?dist}
Summary:        Multi-backend AI coding session daemon

License:        Polyform-Noncommercial-1.0
URL:            https://github.com/dmz006/datawatch
Source0:        https://github.com/dmz006/datawatch/archive/v%{version}.tar.gz

BuildRequires:  golang >= 1.22
Requires:       java-17-openjdk-headless
Requires:       tmux >= 3.0

%description
datawatch bridges messaging platforms to AI coding sessions running in tmux.
Message yourself to start coding tasks, receive async updates, and interact
with sessions from anywhere. Supports Signal, Telegram, Matrix, webhooks and more.

%prep
%autosetup -n datawatch-%{version}

%build
go build -ldflags="-X main.Version=%{version}" \
  -o datawatch ./cmd/datawatch/

%install
install -Dm755 datawatch %{buildroot}%{_bindir}/datawatch
install -Dm644 install/systemd/datawatch.service \
  %{buildroot}%{_unitdir}/datawatch.service
install -dm755 %{buildroot}%{_sysconfdir}/datawatch
install -dm755 %{buildroot}%{_sharedstatedir}/datawatch

%pre
getent group datawatch >/dev/null || groupadd -r datawatch
getent passwd datawatch >/dev/null || \
  useradd -r -g datawatch -d %{_sharedstatedir}/datawatch \
    -s /sbin/nologin -c "datawatch daemon" datawatch
exit 0

%post
%systemd_post datawatch.service

%preun
%systemd_preun datawatch.service

%postun
%systemd_postun_with_restart datawatch.service

%files
%license LICENSE
%doc README.md
%{_bindir}/datawatch
%{_unitdir}/datawatch.service
%dir %{_sysconfdir}/datawatch
%attr(0750,datawatch,datawatch) %dir %{_sharedstatedir}/datawatch

%changelog
* Wed Jan 01 2025 dmz006 <dmz006@github.com> - 0.1.0-1
- Initial package release
