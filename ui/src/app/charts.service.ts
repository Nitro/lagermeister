import { Injectable } from '@angular/core';
import { Http } from '@angular/http';
import { Observable, Subject, Observer } from 'rxjs/Rx';

import * as _ from "lodash";

@Injectable()
export class ChartsService {

    hostAddress: string = 'localhost:9010';
    chartConfig: any = '';
    observable: any;

    constructor(private http: Http) {
        this.create('ws://' + this.hostAddress + '/messages');
    }

    /**
     * Create a web socket connection and set the observable
     * @param url
     * @returns {any}
     */
    private create(url: any): Subject<MessageEvent> {
        let ws = new WebSocket(url);

        this.observable = Observable.create(
            (obs: Observer<MessageEvent>) => {
                ws.onmessage = obs.next.bind(obs);
                ws.onerror = obs.error.bind(obs);
                ws.onclose = obs.complete.bind(obs);
                return ws.close.bind(ws);
            }
        );
        let observer = {
            next: (data: Object) => {
                if (ws.readyState === WebSocket.OPEN) {
                    ws.send(data);
                }
            },
        };
        return Subject.create(observer, this.observable);
    }

    /**
     * Return the observable object
     * @returns {any}
     */
    getObservable() {
        return this.observable;
    }

    /**
     * Fetch the chart config data
     * @returns {Promise<T>}
     */
    fetchConfig(): Promise<any> {
        return new Promise((resolve, reject) => {
            this.http.get('http://' + this.hostAddress + '/config')
                .subscribe(
                    (response: any) => {
                        this.chartConfig = JSON.parse(response.text());
                        resolve(this.chartConfig);
                    },
                    (err: any) => {
                        reject(err);
                    });
        })
    }

    /**
     * Load the google scripts and bind to callback function
     * @param callBackFn
     */
    loadGoogleScripts(callBackFn: any) {
        // if (!this.googleChartsLoaded) {
        //     let script = document.createElement('script');
        //     script.src = '//www.google.com/jsapi';
        //     script.onload = () => {
        //         (<any>window).google.charts.load("visualization", "1", {packages: ["corechart", "gauge"]});
        //         (<any>window).google.charts.setOnLoadCallback(callBackFn.bind(this));
        //         this.googleChartsLoaded = true;
        //     };
        //     document.head.appendChild(script);
        // } else {
        //     (<any>window).google.charts.setOnLoadCallback(callBackFn.bind(this));
        // }
    }

    /**
     * Returns the shared chart options
     */
    getLineChartOptions() {
        return {
            isStacked: true,
            legend: 'top',
            chartArea: {top: 50, left: 100, right: 120},
            height: 250,
            curveType: 'function',
            hAxis: {
                gridLines: {color: '#333'},
                viewWindow: {}
            },
            vAxis: {
                gridLines: {color: '#333'},
                viewWindow: {
                    min: 0
                }
            }
        };
    }

    /**
     * Function used for the inital drawing of the lineChart
     * @param chartObj
     */
    drawLineChart(chartObj: any) {
        chartObj.data = new (<any>window).google.visualization.DataTable();
        let googleChart;

        googleChart = new (<any>window).google.visualization.AreaChart(document.getElementsByClassName(chartObj.domElement)[0]);

        chartObj.columns.forEach((col:Array<string>) => {
            chartObj.data.addColumn(col[1], col[0]);
        });

        googleChart.draw(chartObj.data,chartObj.options);
        chartObj.chart = googleChart;
        chartObj.chartReady = true;
    }

    /**
     * Updateh the line chart data for a given chart
     * @param data
     * @param chartObj
     */
    updateLineChart(data: any, chartObject: any) {
        let date = new Date(data.Timestamp * 1000);
        let min = new Date((data.Timestamp-60) * 1000);

        // Moves the viewWindow to give us the effect that the chart is scrolling
        chartObject.options.hAxis.viewWindow.min = min;
        chartObject.options.hAxis.viewWindow.max = date;

        if (!_.isNil(data.Values)) { // Draw multiple lines
            let fields = [date];
            for (let i in data.Values) {
                fields.push(data.Values[i])
            }
            chartObject.data.addRow(fields);
        } else { // Draw single line
            chartObject.data.addRow([date, data.Value]);
        }
        chartObject.chart.draw(chartObject.data, chartObject.options);

        // Remove any excess rows over 200 (already out of view) to stop chart data getting too big
        if (chartObject.data.getNumberOfRows() > 200) {
            chartObject.data.removeRow(0);
        }
    }

    /**
     * Function used for the inital drawing of the gauge
     * @param gaugeObject
     */
    drawGauge(gaugeObject: any) {
        gaugeObject.data = new (<any>window).google.visualization.DataTable();
        let googleChart;

        googleChart = new (<any>window).google.visualization.Gauge(document.getElementsByClassName(gaugeObject.domElement)[0]);

        gaugeObject.columns.forEach((col:Array<string>) => {
            gaugeObject.data.addColumn(col[1], col[0]);
        });

        gaugeObject.data.addRow(['', 0]);

        googleChart.draw(gaugeObject.data, gaugeObject.options);
        gaugeObject.chart = googleChart;
        gaugeObject.chartReady = true;
    }

    /**
     * Update the gauge data for a given chart
     * @param data
     * @param chartObj
     */
    updateGauge(data: any, chartObject: any) {
        let extraOptions = {};
        if (data.Threshold) {
            extraOptions = {
                redFrom: data.Threshold.Error,
                redTo: data.Value > 150 ? data.Value : 150,
                yellowFrom: data.Threshold.Warn,
                yellowTo: data.Threshold.Error,
                max: data.Value > 150 ? data.Value : 150
            };
        }

        chartObject.data.setValue(0, 1, parseFloat(data.Value));
        chartObject.chart.draw(chartObject.data, Object.assign(chartObject.options, extraOptions));
    }
}
